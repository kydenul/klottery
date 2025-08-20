package lottery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFastRandomGenerator(t *testing.T) {
	fastGen := NewSecureRandomGenerator(100)

	t.Run("范围生成正确性", func(t *testing.T) {
		for i := 0; i < 1000; i++ {
			result, err := fastGen.GenerateInRange(1, 100)
			require.NoError(t, err)
			require.GreaterOrEqual(t, result, 1)
			require.LessOrEqual(t, result, 100)
		}
	})

	t.Run("浮点生成正确性", func(t *testing.T) {
		for i := 0; i < 1000; i++ {
			result, err := fastGen.GenerateFloat()
			require.NoError(t, err)
			require.GreaterOrEqual(t, result, 0.0)
			require.Less(t, result, 1.0)
		}
	})

	t.Run("缓存重填充", func(t *testing.T) {
		// 消耗所有缓存
		for i := 0; i < 150; i++ { // 超过缓存大小
			_, err := fastGen.GenerateFloat()
			require.NoError(t, err)
		}
	})
}
