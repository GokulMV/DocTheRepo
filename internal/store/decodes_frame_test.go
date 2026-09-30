package store

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestFrameTarget(t *testing.T) {
	cases := []struct {
		f                   ports.StackFrame
		file, fn, qualified string
	}{
		{ports.StackFrame{Module: "billing.invoice", Function: "render", File: "/app/billing/invoice.py"}, "app/billing/invoice.py", "render", "render"},
		{ports.StackFrame{Module: "com.acme.orders.Repo", Function: "save", File: "Repo.java"}, "com/acme/orders/Repo.java", "save", "Repo.save"},
		{ports.StackFrame{Module: "com.acme.orders.Repo$Inner", Function: "run", File: "Repo.java"}, "com/acme/orders/Repo.java", "run", "Repo.run"},
		{ports.StackFrame{Function: "OrderService.refund", File: "./src/orders/service.ts"}, "src/orders/service.ts", "refund", "OrderService.refund"},
		{ports.StackFrame{Module: "github.com/acme/api/handlers", Function: "Create", File: "/go/src/api/handlers/orders.go"}, "go/src/api/handlers/orders.go", "Create", "Create"},
	}
	for _, c := range cases {
		file, fn, q := frameTarget(c.f)
		assert.Equal(t, [3]string{c.file, c.fn, c.qualified}, [3]string{file, fn, q}, "%+v", c.f)
	}
}
