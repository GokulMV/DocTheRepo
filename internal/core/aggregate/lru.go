package aggregate

import "container/list"

// lru is a bounded map that evicts the least recently used key. Not safe for concurrent use.
type lru[V any] struct {
	cap   int
	ll    *list.List
	items map[string]*list.Element
}

type entry[V any] struct {
	key string
	val V
}

func newLRU[V any](capacity int) *lru[V] {
	return &lru[V]{cap: capacity, ll: list.New(), items: map[string]*list.Element{}}
}

func (c *lru[V]) get(k string) (V, bool) {
	if e, ok := c.items[k]; ok {
		c.ll.MoveToFront(e)
		return e.Value.(*entry[V]).val, true
	}
	var zero V
	return zero, false
}

func (c *lru[V]) put(k string, v V) {
	if e, ok := c.items[k]; ok {
		e.Value.(*entry[V]).val = v
		c.ll.MoveToFront(e)
		return
	}
	c.items[k] = c.ll.PushFront(&entry[V]{key: k, val: v})
	if c.ll.Len() > c.cap {
		last := c.ll.Back()
		c.ll.Remove(last)
		delete(c.items, last.Value.(*entry[V]).key)
	}
}

func (c *lru[V]) len() int { return c.ll.Len() }
