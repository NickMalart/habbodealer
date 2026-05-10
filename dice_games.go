package main

import (
	"fmt"
	"strings"
)

// Double Trouble (2 Dice) Rules:
// Win: Any Even total (2, 4, 6, 8, 10, 12).
// Lose: Any Odd total (3, 5, 9, 11).
// House Win: If they roll exactly 7.

type GameResult int

const (
	PlayerWin GameResult = iota
	PlayerLose
	HouseWin
)

func evaluateDoubleTrouble(dice1, dice2 int) (int, GameResult) {
	total := dice1 + dice2
	if total == 7 {
		return total, HouseWin
	}
	if total%2 == 0 {
		return total, PlayerWin
	}
	return total, PlayerLose
}

// Triple Trouble (3 Dice) Rules:
// Win: Any Even total.
// Lose: Any Odd total (except 11).
// House Win: If they roll exactly 11.

func evaluateTripleTrouble(dice1, dice2, dice3 int) (int, GameResult) {
	total := dice1 + dice2 + dice3
	if total == 11 {
		return total, HouseWin
	}
	if total%2 == 0 {
		return total, PlayerWin
	}
	return total, PlayerLose
}

func (a *App) evaluateDoubleTroubleRound() {
    if len(diceList) < 2 {
        a.AddLogMsg("[DT] Error: not enough dice found for Double Trouble")
        return
    }

    // Assuming first two dice are used for this game.
    dice1 := diceList[0].Value
    dice2 := diceList[1].Value

    total, result := evaluateDoubleTrouble(dice1, dice2)

    playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

    var winnerName string
    var playerWins bool
    switch result {
    case PlayerWin:
        winnerName = playerName
        playerWins = true
    case PlayerLose:
        winnerName = "Dealer"
        playerWins = false
    case HouseWin:
        winnerName = "Dealer" // House win is a dealer win
        playerWins = false
    }

    resultText := fmt.Sprintf("%d", total)
    winnerMsg := fmt.Sprintf("%s Wins - Total: %s", winnerName, resultText)

    if !ChatIsDisabled {
		if !isMuted {
			sendMessageWithDelay(winnerMsg)
		} else {
			messageQueue = append(messageQueue, winnerMsg)
		}
	}

    payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName

    a.setCurrentGameHistoryResults(resultText, "", winnerName, "Completed", !playerWins)
    a.noteCurrentGameHistory(winnerMsg)

	if playerWins && payoutTargetID > 0 {
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] dt player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		resetPayoutRetryState()
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

    go a.openDealerAfterRound()
}

func (a *App) evaluateTripleTroubleRound() {
    if len(diceList) < 3 {
        a.AddLogMsg("[TT] Error: not enough dice found for Triple Trouble")
        return
    }

    // Assuming first three dice are used for this game.
    dice1 := diceList[0].Value
    dice2 := diceList[1].Value
    dice3 := diceList[2].Value

    total, result := evaluateTripleTrouble(dice1, dice2, dice3)

    playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

    var winnerName string
    var playerWins bool
    switch result {
    case PlayerWin:
        winnerName = playerName
        playerWins = true
    case PlayerLose:
        winnerName = "Dealer"
        playerWins = false
    case HouseWin:
        winnerName = "Dealer" // House win is a dealer win
        playerWins = false
    }

    resultText := fmt.Sprintf("%d", total)
    winnerMsg := fmt.Sprintf("%s Wins - Total: %s", winnerName, resultText)

    if !ChatIsDisabled {
		if !isMuted {
			sendMessageWithDelay(winnerMsg)
		} else {
			messageQueue = append(messageQueue, winnerMsg)
		}
	}

    payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName

    a.setCurrentGameHistoryResults(resultText, "", winnerName, "Completed", !playerWins)
    a.noteCurrentGameHistory(winnerMsg)

	if playerWins && payoutTargetID > 0 {
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] tt player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		resetPayoutRetryState()
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

    go a.openDealerAfterRound()
}
