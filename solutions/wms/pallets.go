package main

import "fmt"

// Input and Output are generated from the controlled code function schemas.
func Run(input Input) (Output, error) {
	if input.Quantity <= 0 || input.UnitsPerPallet <= 0 {
		return Output{}, fmt.Errorf("quantity and unitsPerPallet must be positive")
	}
	pallets := input.Quantity / input.UnitsPerPallet
	if input.Quantity%input.UnitsPerPallet != 0 {
		pallets++
	}
	return Output{Pallets: pallets}, nil
}
