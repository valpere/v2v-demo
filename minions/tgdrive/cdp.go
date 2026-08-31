// cdp.go — minimal Chrome DevTools Protocol client for driving an already-open,
// logged-in web.telegram.org/a/ tab (chromium --remote-debugging-port=9222).
// Used to run docs/smoke-test.md against the live bot as a real user.
//
//	go run . eval  '<js>'      run JS in the page, print its value
//	go run . type  '<text>'    Input.insertText into the focused element
//	go run . key   '<Key>'     press a key (Enter/Escape)
//	go run . click 'x,y'       left click at page coords
//	go run . send  '<text>'    focus the composer, type, press Enter
//	go run . read  <N>         print the last N messages as "IN|OUT<tab>text"
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func tgPageWS() string {
	resp, err := http.Get("http://localhost:9222/json/list")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var raw []map[string]any
	json.Unmarshal(body, &raw)
	for _, t := range raw {
		if t["type"] == "page" && strings.Contains(fmt.Sprint(t["url"]), "web.telegram.org") {
			return fmt.Sprint(t["webSocketDebuggerUrl"])
		}
	}
	log.Fatalf("no web.telegram.org page tab\n%s", body)
	return ""
}

type conn struct {
	c  *websocket.Conn
	id int
}

func (co *conn) call(method string, params map[string]any) map[string]any {
	co.id++
	id := co.id
	m := map[string]any{"id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	if err := co.c.WriteJSON(m); err != nil {
		log.Fatalf("write: %v", err)
	}
	for {
		_, data, err := co.c.ReadMessage()
		if err != nil {
			log.Fatalf("read: %v", err)
		}
		var msg map[string]any
		json.Unmarshal(data, &msg)
		if fid, ok := msg["id"].(float64); ok && int(fid) == id {
			if e, ok := msg["error"]; ok {
				log.Fatalf("%s error: %v", method, e)
			}
			r, _ := msg["result"].(map[string]any)
			return r
		}
	}
}

func merge(a, b map[string]any) map[string]any {
	m := map[string]any{}
	for k, v := range a {
		m[k] = v
	}
	for k, v := range b {
		m[k] = v
	}
	return m
}

func main() {
	if len(os.Args) < 3 {
		log.Fatal("usage: go run . <eval|type|key|click|send|read> <arg>")
	}
	mode, arg := os.Args[1], os.Args[2]

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, _, err := websocket.DefaultDialer.DialContext(ctx, tgPageWS(), nil)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer c.Close()
	co := &conn{c: c}

	switch mode {
	case "eval":
		r := co.call("Runtime.evaluate", map[string]any{
			"expression": arg, "returnByValue": true, "awaitPromise": true, "userGesture": true,
		})
		res, _ := r["result"].(map[string]any)
		if ed, ok := r["exceptionDetails"]; ok {
			log.Fatalf("JS exception: %v", ed)
		}
		if v, ok := res["value"]; ok {
			b, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Printf("(%v)\n", res["type"])
		}
	case "type":
		co.call("Input.insertText", map[string]any{"text": arg})
		fmt.Println("typed:", arg)
	case "click": // arg = "x,y"
		var x, y float64
		fmt.Sscanf(arg, "%f,%f", &x, &y)
		base := map[string]any{"x": x, "y": y, "button": "left", "clickCount": 1}
		co.call("Input.dispatchMouseEvent", merge(base, map[string]any{"type": "mouseMoved"}))
		co.call("Input.dispatchMouseEvent", merge(base, map[string]any{"type": "mousePressed"}))
		co.call("Input.dispatchMouseEvent", merge(base, map[string]any{"type": "mouseReleased"}))
		fmt.Println("clicked:", arg)
	case "key":
		for _, ev := range []string{"keyDown", "keyUp"} {
			p := map[string]any{"type": ev, "key": arg}
			switch arg {
			case "Enter":
				p["code"] = "Enter"
				p["windowsVirtualKeyCode"] = 13
				p["text"] = "\r"
			case "Escape":
				p["code"] = "Escape"
				p["windowsVirtualKeyCode"] = 27
			}
			co.call("Input.dispatchKeyEvent", p)
		}
		fmt.Println("key:", arg)
	case "send": // focus composer, type arg, press Enter
		co.call("Runtime.evaluate", map[string]any{
			"expression":  `(() => { const e=document.querySelector('#editable-message-text'); e.focus(); e.textContent=''; return e.id; })()`,
			"userGesture": true,
		})
		co.call("Input.insertText", map[string]any{"text": arg})
		time.Sleep(150 * time.Millisecond)
		for _, ev := range []string{"keyDown", "keyUp"} {
			co.call("Input.dispatchKeyEvent", map[string]any{
				"type": ev, "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13, "text": "\r",
			})
		}
		fmt.Println("sent:", arg)
	case "read": // last N messages as "IN|OUT\ttext"
		var n int
		fmt.Sscanf(arg, "%d", &n)
		if n == 0 {
			n = 6
		}
		js := fmt.Sprintf(`(() => {
			const ms = [...document.querySelectorAll('.message-list-item, [class*="Message-module"], .Message')];
			return ms.slice(-%d).map(m => {
				const own = m.classList.contains('own') || m.querySelector('.own') || /(^|\s)own(\s|$)/.test(m.className);
				const t = (m.querySelector('.text-content, [class*="text-content"]') || m).innerText || '';
				return (own ? 'OUT\t' : 'IN\t') + t.replace(/\s+/g,' ').trim();
			}).filter(s => s.length > 5);
		})()`, n)
		r := co.call("Runtime.evaluate", map[string]any{"expression": js, "returnByValue": true})
		res, _ := r["result"].(map[string]any)
		if arr, ok := res["value"].([]any); ok {
			for _, v := range arr {
				fmt.Println(v)
			}
		}
	default:
		log.Fatalf("unknown mode %q", mode)
	}
}
