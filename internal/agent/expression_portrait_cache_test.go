package agent

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpressionPortraitThumbCache_putGetAndEvict(t *testing.T) {
	t.Parallel()
	c := newExpressionPortraitThumbCache(2)
	require.NotNil(t, c)

	c.put("a/b", []byte{1, 2}, "image/jpeg")
	raw, mt, ok := c.get("a/b")
	require.True(t, ok)
	require.Equal(t, []byte{1, 2}, raw)
	require.Equal(t, "image/jpeg", mt)

	c.put("c/d", []byte{3}, "")
	raw2, mt2, ok := c.get("c/d")
	require.True(t, ok)
	require.Equal(t, []byte{3}, raw2)
	require.Equal(t, "image/jpeg", mt2)

	c.put("e/f", []byte{4}, "image/png")
	_, _, ok = c.get("a/b")
	require.False(t, ok, "oldest entry evicted at capacity")
	_, _, ok = c.get("c/d")
	require.True(t, ok, "non-evicted key still present")

	raw3, _, ok := c.get("e/f")
	require.True(t, ok)
	require.Equal(t, []byte{4}, raw3)
}

func TestExpressionPortraitThumbCache_getReturnsCopy(t *testing.T) {
	t.Parallel()
	c := newExpressionPortraitThumbCache(10)
	data := []byte{9, 9, 9}
	c.put("u/i", data, "image/jpeg")
	raw, _, ok := c.get("u/i")
	require.True(t, ok)
	raw[0] = 1
	raw2, _, _ := c.get("u/i")
	require.Equal(t, byte(9), raw2[0])
}

func TestExpressionPortraitThumbCache_keyOrderCapStaysBounded(t *testing.T) {
	t.Parallel()
	c := newExpressionPortraitThumbCache(3)
	require.NotNil(t, c)

	for i := 0; i < 8000; i++ {
		c.put(fmt.Sprintf("k%d", i), []byte{byte(i % 255)}, "image/jpeg")
	}
	require.LessOrEqual(t, len(c.keyOrder), c.maxKeys)
	require.LessOrEqual(t, cap(c.keyOrder), 2*c.maxKeys)
}
