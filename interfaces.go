package lottery

import "context"

// ProgressCallback defines the callback function for multi-draw progress updates
type ProgressCallback func(completed, total int, currentResult any)

// LotteryDrawer defines the interface for lottery drawing operations
type LotteryDrawer interface {
	// DrawInRange draws a random number within the specified range
	DrawInRange(ctx context.Context, lockKey string, min, max int) (int, error)

	// DrawMultipleInRange draws multiple random numbers within the specified range
	DrawMultipleInRange(ctx context.Context, lockKey string, min, max, count int) ([]int, error)

	// DrawFromPrizes draws a prize from the given prize pool
	DrawFromPrizes(ctx context.Context, lockKey string, prizes []Prize) (*Prize, error)

	// DrawMultipleFromPrizes draws multiple prizes from the given prize pool
	DrawMultipleFromPrizes(ctx context.Context, lockKey string, prizes []Prize, count int) ([]*Prize, error)

	// DrawMultipleInRangeWithRecovery draws multiple random numbers with enhanced error handling
	DrawMultipleInRangeWithRecovery(ctx context.Context, lockKey string, min, max, count int) (*MultiDrawResult, error)

	// DrawMultipleFromPrizesWithRecovery draws multiple prizes with enhanced error handling
	DrawMultipleFromPrizesWithRecovery(ctx context.Context, lockKey string, prizes []Prize, count int) (*MultiDrawResult, error)

	// ResumeMultiDrawInRange resumes a previously interrupted multi-draw range operation
	ResumeMultiDrawInRange(ctx context.Context, lockKey string, min, max, count int) (*MultiDrawResult, error)

	// ResumeMultiDrawFromPrizes resumes a previously interrupted multi-draw prize operation
	ResumeMultiDrawFromPrizes(ctx context.Context, lockKey string, prizes []Prize, count int) (*MultiDrawResult, error)

	// RollbackMultiDraw attempts to rollback a partially completed multi-draw operation
	RollbackMultiDraw(ctx context.Context, drawState *DrawState) error

	// SaveDrawState saves the current state of a multi-draw operation
	SaveDrawState(ctx context.Context, drawState *DrawState) error

	// LoadDrawState loads a previously saved draw state
	LoadDrawState(ctx context.Context, lockKey string) (*DrawState, error)

	// DrawMultipleInRangeOptimized draws multiple random numbers with performance optimizations
	DrawMultipleInRangeOptimized(ctx context.Context, lockKey string, min, max, count int, progressCallback ProgressCallback) (*MultiDrawResult, error)

	// DrawMultipleFromPrizesOptimized draws multiple prizes with performance optimizations
	DrawMultipleFromPrizesOptimized(ctx context.Context, lockKey string, prizes []Prize, count int, progressCallback ProgressCallback) (*MultiDrawResult, error)
}

// Logger defines the interface for logging operations
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
	Debug(msg string, args ...any)
}
