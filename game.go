//Utility Functions
package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
)

//=============================================
//===============TYPES AND CONSTS==============
//=============================================
type roundName int
type state int
type money uint64
type guid string

const SEED int64 = 0 // seed for deal
var UNSHUFFLED = generateCardNames()

const (
	fold int = iota
	bet
	call
)
const (
	active state = iota
	folded
	called
)
const BUY_IN money = 500

type Player struct {
	state    state
	guid     guid
	wealth   money
	bestHand Hand
}

//==========================================
//===============GAME CLASS=================
//==========================================
type Game struct {
	table      *Table
	pot        *Pot
	gameID     guid
	deck       Deck
	round      uint
	smallBlind money
	controller *controller
	random     *rand.Rand
}

func (g *Game) deal() {
	g.deck = make(Deck, 52)
	numPlayers := len(g.table.players)
	rand_ints := g.random.Perm(52)
	for i := 0; i < numPlayers; i++ {
		card1, card2 := UNSHUFFLED[rand_ints[i*2]], UNSHUFFLED[rand_ints[i*2+1]]
		g.deck[card1] = string(g.table.players[i].guid)
		g.deck[card2] = string(g.table.players[i].guid)
	}
	n := numPlayers * 2
	g.deck[UNSHUFFLED[rand_ints[n+0]]] = "FLOP"
	g.deck[UNSHUFFLED[rand_ints[n+1]]] = "FLOP"
	g.deck[UNSHUFFLED[rand_ints[n+2]]] = "FLOP"
	g.deck[UNSHUFFLED[rand_ints[n+3]]] = "TURN"
	g.deck[UNSHUFFLED[rand_ints[n+4]]] = "RIVER"

	g.table.assignBestHands(g.deck)
}

func newPot() *Pot {
	pot := new(Pot)
	pot.bets = make([]Bet, 0)
	return pot
}

func (g *Game) run() {
	//Testing Stuff
	defer gamePrinter(g)
	reader := bufio.NewReader(os.Stdin)
	//----
	println(">Game Started")
	for {
		g.addWaitingPlayersToGame()
		if len(g.table.players) < 2 {
			continue //Need 2 players to start a hand
		}
		g.table.AdvanceButton()
		g.pot = newPot()
		g.removeBrokePlayers()
		g.betBlinds()
		g.deal()
		println("beforehand>")
		_, _ = reader.ReadString('\n')
		gamePrinter(g)
		for g.round = 0; !g.allFolded() && g.round < 4; g.round++ {
			g.Bets()
			g.table.ResetRound()
			g.pot.newRound()
		}
		g.resolveBets()
		println("afterhand>")
		_, _ = reader.ReadString('\n')
		gamePrinter(g)
		g.table.ResetHand()
	}
}

// resolveBets loops through all sidepots. For each sidepot,
// among the stakeholders, the pot is distributed to the winner(s).
func (g *Game) resolveBets() {
	moneyInPots := g.pot.amounts()

	for potNumber, guids := range g.pot.stakeholders() {
		sidepot := moneyInPots[potNumber]
		players := g.table.getPlayers(guids)
		winners := findWinners(players)
		numWinners := money(len(winners))
		for _, p := range winners {
			p.wealth += sidepot / numWinners
			if sidepot%numWinners > 0 {
				p.wealth++
				moneyInPots[potNumber]--
			}
		}
	}
}

//allFolded returns true if all players have folded.
func (g *Game) allFolded() bool {
	numFolded := 0
	for _, p := range g.table.players {
		if p.state == folded {
			numFolded++
		}
	}
	return numFolded == len(g.table.players)
}

func (g *Game) addWaitingPlayersToGame() {
	numPlayersNeeded := (10 - len(g.table.players))
	newPlayers := g.controller.getNewPlayers(g, numPlayersNeeded)
	for _, p := range newPlayers {
		err := g.table.addPlayer(p.guid)
		if err != nil {
			panic(err)
		}
	}
}

func (g *Game) removeBrokePlayers() {
	for _, p := range g.table.players {
		if p.wealth == 0 {
			p.state = folded
			g.controller.removePlayerFromGame(g, p.guid)
		} else if p.wealth < 0 {
			panic("player has < 0 wealth!")
		}
	}
}

func (g *Game) betBlinds() {
	//Bet small blind
	player := g.table.Next()
	if player.wealth >= g.smallBlind {
		g.pot.commitBet(player, g.smallBlind)
	} else {
		g.pot.commitBet(player, player.wealth)
	}

	//Bet big blind
	player = g.table.Next()
	if player.wealth >= 2*g.smallBlind {
		g.pot.commitBet(player, 2*g.smallBlind)
	} else {
		g.pot.commitBet(player, player.wealth)
	}
}

//setBlinds sets the money amount for the blinds
// and rotates the "button"
func (g *Game) setBlinds() {
	g.smallBlind = 25
}

func (g *Game) betsNeeded() bool {
	numActives := 0
	numFolded := 0
	for _, p := range g.table.players {
		if p.state == active {
			numActives++
		} else if p.state == folded {
			numFolded++
		}
	}
	return (numActives >= 1) && ((len(g.table.players) - numFolded) > 1)
}

//Bets gets the bet from each player
func (g *Game) Bets() {
	for player := g.table.Next(); g.betsNeeded(); player = g.table.Next() {
		if player.state != active {
			continue
		}
		fmt.Printf("asking %v for bet>\n", player.guid)
		gamePrinter(g)
		action, betAmount, err := g.controller.getPlayerBet(g, player.guid)

		//Illegit bets
		if err != nil {
			//Err occurs on connection timeout
			player.state = folded
			g.controller.removePlayerFromGame(g, player.guid)
			continue
		}
		if action == fold {
			player.state = folded
			continue
		}
		if g.pot.betInvalid(player, betAmount) {
			fmt.Printf("player %v has bet an invalid amount of %v; defaulting to fold...\n", player.guid, betAmount)
			g.controller.registerInvalidBet(g, player.guid, betAmount)
			player.state = folded
			continue
		}

		//Legit bets
		if g.pot.raiseAmount(player.guid, betAmount) > 0 {
			g.table.ResetRoundPlayerState()
		}
		g.pot.commitBet(player, betAmount)
		player.state = called
	}

}
