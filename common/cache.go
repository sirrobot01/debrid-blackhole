package common

import (
	"sync"
)

type Cache struct {
	data     map[string]struct{}
	order    []string
	maxItems int
	mu       sync.RWMutex
}

func NewCache(maxItems int) *Cache {
	if maxItems <= 0 {
		maxItems = 1000
	}
	return &Cache{
		data:     make(map[string]struct{}, maxItems),
		order:    make([]string, 0, maxItems),
		maxItems: maxItems,
	}
}

func (c *Cache) Add(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.data[value]; !exists {
		if len(c.order) >= c.maxItems {
			delete(c.data, c.order[0])
			c.order = c.order[1:]
		}
		c.data[value] = struct{}{}
		c.order = append(c.order, value)
	}
}

func (c *Cache) AddMultiple(values map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for value, exists := range values {
		if !exists {
			if _, exists := c.data[value]; !exists {
				if len(c.order) >= c.maxItems {
					delete(c.data, c.order[0])
					c.order = c.order[1:]
				}
				c.data[value] = struct{}{}
				c.order = append(c.order, value)
			}
		}
	}
}

func (c *Cache) Get(index int) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if index < 0 || index >= len(c.order) {
		return "", false
	}
	return c.order[index], true
}

func (c *Cache) GetMultiple(values []string) map[string]bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]bool, len(values))
	for _, value := range values {
		if _, exists := c.data[value]; exists {
			result[value] = true
		}
	}
	return result
}

func (c *Cache) Exists(value string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.data[value]
	return exists
}

func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.order)
}
