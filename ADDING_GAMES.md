# Adding a New Dice Game to HabboDealer

This guide explains the steps required to implement a new dice game in the HabboDealer codebase. Adding a game involves updating the backend state, rolling logic, evaluation rules, and frontend UI.

## 1. Backend: State and Configuration (`main.go`)

### A. State Variables
Add the necessary variables to the `App` struct or global scope to track the game state.
```go
// Example for a game called "MyGame"
myGameRoundActive            bool
myGamePlayerTurn             bool
myGamePlayerTotal            int
myGameDealerTotal            int
myGamePlayerName             string
isMyGameRolling              bool
isMyGameHitting              bool
myGameHitInFlight            bool
myGameNextHitIndex           int
```

### B. Game Enabling Flags
Add a boolean flag and update `setEnabledGamesFromSelection`.
```go
enabledGameMyGame bool = true

func setEnabledGamesFromSelection(codes []string) {
    // ... reset flags ...
    case "mygame":
        enabledGameMyGame = true
}
```

### C. Reset Logic
Include your new flags in `resetDiceState` and create a dedicated `resetMyGameSequence` function.
```go
func resetMyGameSequence() {
    myGameRoundActive = false
    myGamePlayerTotal = 0
    isMyGameRolling = false
    // ... reset other flags ...
}
```

## 2. Backend: Rolling and Waiting (`main.go`)

### A. Rolling Functions
Implement `rollMyGameDice` and `hitMyGameDice`. Ensure you use `resultsWaitGroup.Add()` and handle `fakeDiceTestingMode`.
**Crucial**: Always use `defer` to reset rolling flags.

```go
func (a *App) rollMyGameDice() {
    defer func() { isMyGameRolling = false }()
    // ... rolling logic ...
    a.waitForMyGameResults([]int{0}, 5*time.Second, "initial-roll")
    a.evaluateMyGameHand()
}
```

### B. Dice Result Handler
Update `handleDiceResult` to include your new rolling flags. **If you miss this, the bot will hang for 5 seconds on every roll.**
```go
if dice.IsRolling && (isPokerRolling || ... || isMyGameRolling) {
    resultsWaitGroup.Done()
}
```

## 3. Backend: Chat and Selection (`main.go`)

### A. Normalization
Update `normalizeIncomingGameChoice` so the bot recognizes the game name from chat.
```go
case "mygame", "mg":
    return "mygame", true
```

### B. Interception (`handleIncomingChat`)
1. Add your flags to the "busy check".
2. Add an acknowledgement message to the `ack` switch.
3. Trigger your sequence in the sequence switch.

## 4. Backend: Rules and Evaluation (`hand.go`)

Implement the core game logic:
1. `evaluateMyGameHand()`: Checks totals, handles busts, and triggers hits or dealer turns.
2. `startMyGameDealerTurn()`: Automated dealer play logic.
3. `finalizeMyGameRound(playerWins bool, reason string)`: Shouts the winner and triggers payout or re-opening.

## 5. Backend: Statistics (`casino_stats.go`)

Update `normalizeGameKey` and the `ByGame` map in `BuildCasinoStats` to include your new game. This ensures the dashboard correctly tracks wins/losses.

## 6. Frontend: UI Integration (`frontend/src/App.vue`)

1. **Guide**: Add an entry to the `gameGuides` array describing the game.
2. **Setup**: Add a checkbox in the "Dealer Setup" modal (`v-model="enableGameMyGame"`).
3. **Data**: Add `enableGameMyGame` to the Vue `data()` object.
4. **Validation**: Update `confirmDealerName` to include your new game in the `selectedGames` array.

## Common Pitfalls
*   **Missing Result Handler**: Forgetting to add `isMyGameRolling` to `handleDiceResult` causes the bot to "wait" forever (until timeout) for dice results.
*   **Dice Requirement**: Ensure your `roll` functions check for the correct number of dice (e.g., `len(diceList) < 1` for single dice games).
*   **Sequence Isolation**: Always call `resetOtherGamesSequence()` when starting a new game to prevent old state (like a "hit" prompt) from carrying over.
