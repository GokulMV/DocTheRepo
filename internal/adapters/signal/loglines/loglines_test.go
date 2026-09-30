package loglines

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/GokulMV/DocTheRepo/internal/core/signals"
)

func TestPython(t *testing.T) {
	p := Parse(`ERROR:app:Unhandled exception
Traceback (most recent call last):
  File "/app/orders/api.py", line 40, in create
    total = pricing.total(cart)
  File "/app/orders/pricing.py", line 12, in total
    return sum(i["price"] for i in cart)
KeyError: 'price'`)
	assert.Equal(t, "ERROR", p.Level)
	assert.Equal(t, "KeyError", p.ExceptionType)
	assert.Equal(t, "'price'", p.Message)
	assert.Equal(t, []string{"/app/orders/pricing.py:total", "/app/orders/api.py:create"}, signals.Frames(p.Stack), "top of stack first")
}

func TestJava(t *testing.T) {
	p := Parse(`2026-09-30 10:00:00 ERROR [http-nio-8080-exec-3] c.a.o.OrderController - Request failed
java.lang.IllegalStateException: order 42 already paid
	at com.acme.orders.OrderService.pay(OrderService.java:88)
	at com.acme.orders.OrderController.pay(OrderController.java:31)
	at java.base/java.lang.Thread.run(Thread.java:1583)`)
	assert.Equal(t, "ERROR", p.Level)
	assert.Equal(t, "java.lang.IllegalStateException", p.ExceptionType)
	assert.Equal(t, "order 42 already paid", p.Message)
	assert.Equal(t, "com.acme.orders.orderservice:pay", signals.Frames(p.Stack)[0])
	assert.Equal(t, 88, p.Stack[0].Line)
}

func TestNodeAndGo(t *testing.T) {
	n := Parse(`TypeError: Cannot read properties of undefined (reading 'id')
    at getUser (/srv/app/users.js:14:22)
    at /srv/app/server.js:50:5`)
	assert.Equal(t, "TypeError", n.ExceptionType)
	assert.Equal(t, "getUser", n.Stack[0].Function)
	assert.Equal(t, 14, n.Stack[0].Line)
	assert.Len(t, n.Stack, 2)

	g := Parse("panic: runtime error: index out of range [3] with length 3\n\ngoroutine 1 [running]:\nmain.pick(...)\n\t/app/main.go:12 +0x1d\nmain.main()\n\t/app/main.go:20 +0x25\n")
	assert.Equal(t, "panic", g.ExceptionType)
	assert.Equal(t, "PANIC", g.Level)
	assert.Equal(t, []string{"main:pick", "main:main"}, signals.Frames(g.Stack))
}

func TestJSONLogs(t *testing.T) {
	p := Parse(`{"level":"error","msg":"charge failed","error":{"type":"StripeTimeout","message":"read timeout"},"service":"billing","attempt":3,
		"stack":"Traceback (most recent call last):\n  File \"/app/b.py\", line 3, in charge\nStripeTimeout: read timeout"}`)
	assert.Equal(t, "error", p.Level)
	assert.Equal(t, "charge failed", p.Message)
	assert.Equal(t, "StripeTimeout", p.ExceptionType)
	assert.Equal(t, "billing", p.Fields["service"])
	assert.Equal(t, "3", p.Fields["attempt"])
	assert.Equal(t, "charge", p.Stack[0].Function)
	dotted := Parse(`{"log.level":"WARN","message":"slow query","error.type":"Timeout"}`)
	assert.Equal(t, "WARN", dotted.Level)
	assert.Equal(t, "Timeout", dotted.ExceptionType)
}

func TestPlainAndFirstLine(t *testing.T) {
	p := Parse("upstream connect error or disconnect/reset before headers")
	assert.Empty(t, p.Level)
	assert.Empty(t, p.ExceptionType)
	assert.Equal(t, "upstream connect error or disconnect/reset before headers", p.Message)
	assert.Equal(t, "a", FirstLine("  a\nb"))
	assert.Equal(t, "{broken", Parse("{broken").Message, "invalid JSON falls back to text")
}
