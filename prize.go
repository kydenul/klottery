package lottery

// Prize represents a lottery prize with probability and value
type Prize struct {
	ID          string  `json:"id"`          // Prize ID
	Name        string  `json:"name"`        // Prize name
	Probability float64 `json:"probability"` // Winning probability (0-1)
	Value       int     `json:"value"`       // Prize value

	// TODO: Add support the number of prizes
	// Number int // Prize number
	// TODO: Add default prize
	// DefaultID string // Default prize
}

// Validate validates the prize data
func (p *Prize) Validate() error {
	if p.ID == "" {
		return ErrInvalidPrizeID
	}
	if p.Name == "" {
		return ErrInvalidPrizeName
	}
	if p.Probability < 0 || p.Probability > 1 {
		return ErrInvalidProbability
	}
	if p.Value < 0 {
		return ErrNegativePrizeValue
	}
	return nil
}

// ValidatePrizePool validates a slice of prizes
func ValidatePrizePool(prizes []Prize) error {
	if len(prizes) == 0 {
		return ErrEmptyPrizePool
	}

	var totalProbability float64
	for _, prize := range prizes {
		if err := prize.Validate(); err != nil {
			return err
		}
		totalProbability += prize.Probability
	}

	// Check if probabilities sum to approximately 1.0 (within tolerance)
	if totalProbability < (1.0-ProbabilityTolerance) || totalProbability > (1.0+ProbabilityTolerance) {
		return ErrInvalidProbability
	}

	return nil
}

// DefaultPrizeSelector implements probability-based prize selection
type DefaultPrizeSelector struct {
	generator *SecureRandomGenerator
}

// NewDefaultPrizeSelector creates a new prize selector
func NewDefaultPrizeSelector() *DefaultPrizeSelector {
	return &DefaultPrizeSelector{
		generator: NewSecureRandomGenerator(),
	}
}

// NormalizeProbabilities normalizes the probabilities in a prize pool to sum to 1.0
func (ps *DefaultPrizeSelector) NormalizeProbabilities(prizes []Prize) ([]Prize, error) {
	if len(prizes) == 0 {
		return nil, ErrEmptyPrizePool
	}

	// Calculate total probability
	var totalProbability float64
	for _, prize := range prizes {
		if prize.Probability < 0 {
			return nil, ErrInvalidProbability
		}
		totalProbability += prize.Probability
	}

	// If total is zero, return error
	if totalProbability == 0 {
		return nil, ErrInvalidProbability
	}

	// Normalize probabilities
	normalizedPrizes := make([]Prize, len(prizes))
	for i, prize := range prizes {
		normalizedPrizes[i] = prize
		normalizedPrizes[i].Probability = prize.Probability / totalProbability
	}

	return normalizedPrizes, nil
}

// CalculateCumulativeProbabilities calculates cumulative probabilities for prize selection
func (ps *DefaultPrizeSelector) CalculateCumulativeProbabilities(prizes []Prize) ([]float64, error) {
	if len(prizes) == 0 {
		return nil, ErrEmptyPrizePool
	}

	cumulativeProbabilities := make([]float64, len(prizes))
	var cumulative float64
	for i, prize := range prizes {
		if prize.Probability < 0 || prize.Probability > 1 {
			return nil, ErrInvalidProbability
		}
		cumulative += prize.Probability
		cumulativeProbabilities[i] = cumulative
	}

	// Ensure the last cumulative probability is exactly 1.0 to handle floating point precision
	if len(cumulativeProbabilities) > 0 {
		cumulativeProbabilities[len(cumulativeProbabilities)-1] = 1.0
	}

	return cumulativeProbabilities, nil
}

// SelectPrize selects a prize from the pool based on probability using cumulative distribution
func (ps *DefaultPrizeSelector) SelectPrize(prizes []Prize) (*Prize, error) {
	if len(prizes) == 0 {
		return nil, ErrEmptyPrizePool
	}

	// Validate and normalize probabilities if needed
	normalizedPrizes, err := ps.NormalizeProbabilities(prizes)
	if err != nil {
		return nil, err
	}

	// Calculate cumulative probabilities
	cumulativeProbabilities, err := ps.CalculateCumulativeProbabilities(normalizedPrizes)
	if err != nil {
		return nil, err
	}

	// Generate random float between 0 and 1
	randomValue, err := ps.generator.GenerateFloat()
	if err != nil {
		return nil, err
	}

	// Find the prize using binary search for efficiency
	selectedIndex := ps.findPrizeIndex(cumulativeProbabilities, randomValue)
	if selectedIndex < 0 || selectedIndex >= len(normalizedPrizes) {
		// Fallback to last prize if index is out of bounds
		selectedIndex = len(normalizedPrizes) - 1
	}

	// Return a copy of the selected prize
	selectedPrize := normalizedPrizes[selectedIndex]
	return &selectedPrize, nil
}

// findPrizeIndex finds the index of the prize to select using binary search
func (ps *DefaultPrizeSelector) findPrizeIndex(cumulativeProbabilities []float64, randomValue float64) int {
	left, right := 0, len(cumulativeProbabilities)-1

	// Binary search for the first cumulative probability >= randomValue
	for left <= right {
		mid := left + (right-left)/2
		if cumulativeProbabilities[mid] >= randomValue {
			// Check if this is the first occurrence
			if mid == 0 || cumulativeProbabilities[mid-1] < randomValue {
				return mid
			}
			right = mid - 1
		} else {
			left = mid + 1
		}
	}

	// Fallback to the last index if not found
	return len(cumulativeProbabilities) - 1
}

// ValidatePrizes validates that the prize pool probabilities are valid
func (ps *DefaultPrizeSelector) ValidatePrizes(prizes []Prize) error {
	if len(prizes) == 0 {
		return ErrEmptyPrizePool
	}

	var totalProbability float64
	for _, prize := range prizes {
		if err := prize.Validate(); err != nil {
			return err
		}
		totalProbability += prize.Probability
	}

	// Check if probabilities sum to approximately 1.0 (within tolerance)
	if totalProbability < (1.0-ProbabilityTolerance) || totalProbability > (1.0+ProbabilityTolerance) {
		return ErrInvalidProbability
	}

	return nil
}

// SelectMultiplePrizes selects multiple prizes from the pool (with replacement)
func (ps *DefaultPrizeSelector) SelectMultiplePrizes(prizes []Prize, count int) ([]*Prize, error) {
	if err := ValidateCount(count); err != nil {
		return nil, err
	}

	results := make([]*Prize, 0, count)
	for range count {
		prize, err := ps.SelectPrize(prizes)
		if err != nil {
			return results, err
		}
		results = append(results, prize)
	}

	return results, nil
}
