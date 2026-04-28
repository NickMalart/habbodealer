package main

import (
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Dealer-configurable Under/Over-7 payout multiplier (2..5). Default 3.
var underOver7PayoutMultiplier int = 3

// SetUnderOver7PayoutMultiplier sets the dealer-selected payout multiplier for
// Under/Over-7 rounds. Constrains to 2..5 and emits an event for the frontend.
func (a *App) SetUnderOver7PayoutMultiplier(mult int) {
	if mult < 2 {
		mult = 2
	}
	if mult > 5 {
		mult = 5
	}

	mutex.Lock()
	underOver7PayoutMultiplier = mult
	mutex.Unlock()

	a.AddLogMsg(fmt.Sprintf("[CONFIG] UnderOver7 payout multiplier set to x%d", mult))
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "underOver7PayoutMultiplierChanged", mult)
	}
}
