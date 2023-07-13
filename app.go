package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"

	elogo "github.com/kortemy/elo-go"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// verbose can be turned on to log calculation output for debugging
var verbose bool = false

// second version of the algorithm, patch version 2
var version = "0.2.3"

// reward curves for different numbers of players in a game
var (
	twoPlayers   = []float64{1.0, 0}
	threePlayers = []float64{1.0, 0.5, 0}
	fourPlayers  = []float64{1.0, 0.5, 0.25, 0}
	fivePlayers  = []float64{1.0, 0.5, 0.25, 0.12, 0}
	sixPlayers   = []float64{1.0, 0.5, 0.25, 0.12, 0.05, 0}
)

// Game is a modeled MTG Game with a set of rankings determined by order of player loss.
type Game struct {
	ID             string   // the ID of the game, which also correlates to its number in the game log.
	Date           string   // the date of the game.
	Rankings       []string // an ordered list of players with index 0 being the winner and each subsequent position the next rank.
	TableZap       string   // marks if the game was ended in one resolution.
	DrawGame       string   // if draw game is marked, the game ended in a draw for all players, so order doesn't matter but players still need to be recorded.
	RankTotal      int      // the total elo scores of the game for determining the skill level of the game.
	RankAverage    int      // the average elo score of the game determined by diviving the number of players from the above rank average.
	TwoHeadedGiant bool     // if the game is a match of multiple players per team, colloquially referred to as a two-headed giant game.
}

// Player binds a calculated score to a player
type Player struct {
	Name  string
	Score int
}

// ByID implements the sort.Interface for sorting games by ID.
type ByID []*Game

// ByScore implements the sort.Interface for sorting players by Score.
type ByScore []Player

//go:embed templates/*
var resources embed.FS
var t = template.Must(template.ParseFS(resources, "templates/*"))

func main() {
	port := os.Getenv("SCOREBOARD_PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// fetch games
		games, err := fetchGameData()
		if err != nil {
			log.Printf("error fetching game data: %+v", err)
			errorRes(w, err)
			return
		}

		// sort by ID to ensure order
		sort.Sort(ByID(games))

		// calculate and render scores
		scores := calculateScores(games)

		// collect and sort players into rankings
		rankings := []Player{}
		for k, v := range scores {
			rankings = append(rankings, Player{
				Name:  k,
				Score: v,
			})
		}

		// sort by score to determine rankings
		sort.Sort(ByScore(rankings))

		// create and format a response object
		data := map[string]interface{}{
			"version":  version,
			"games":    games,
			"scores":   scores,
			"rankings": rankings,
			"total":    len(games),
		}
		if verbose {
			log.Printf("%s", data)
		}
		w.Header().Add("X-PoweredBy", "stamina_crü") // 💪
		t.ExecuteTemplate(w, "index.html.tmpl", data)
	})

	log.Println("listening on", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func errorRes(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	data := map[string]string{
		"version": version,
		"errors":  err.Error(),
	}
	t.ExecuteTemplate(w, "index.html.tmpl", data)
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// fetchGameData fetches the raw CSV data from Google Sheets API and then
// parses it and returns a list of games or an error.
func fetchGameData() ([]*Game, error) {
	ctx := context.Background()
	b, err := os.ReadFile("credentials.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read client secret file: %w", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, "https://www.googleapis.com/auth/spreadsheets.readonly")
	if err != nil {
		return nil, fmt.Errorf("failed to parse client secret file to config: %w", err)
	}
	client := getClient(config)

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve Google Sheets client: %w", err)
	}

	// NOTE: spreadsheetId for the game tracker
	spreadsheetID := "1-qr-ejHx07Hrr35OymMcGRH00-Jzb-k8S8-xS9P5vqk"

	readRange := "Ranked game log!A:K"
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, readRange).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve data from sheet: %w", err)
	}
	if len(resp.Values) == 0 {
		return nil, fmt.Errorf("no game data found")
	}

	games, err := parseGameData(resp.Values)
	if err != nil {
		return nil, err
	}

	return games, nil
}

// parseGame is responsible for parsing the raw game data that we get from
// Google Sheets.
func parseGameData(values [][]interface{}) ([]*Game, error) {
	var games []*Game
	for idx, row := range values {
		if len(row) < 4 {
			log.Printf("encountered malformed row %+v at %+v", row, idx)
			continue
		}
		if idx == 0 {
			// skip the first row, it contains the game sheet labels
			continue
		}

		// This function assumes a CSV sheet with the following schema.
		// * Each row is a game in the data.
		// * Columns C through H map to in-order player rankings for a given game.
		// * This schema supports up to 6 players, because we only have calculated
		// reward curves for up to 6 players, and there is a drastic drop off in
		// quantity of games after 4 players, which is the overwhelming average
		// pod size. The column schema then looks like below.
		// * column schema: |    A	 | 	 B 	|   C  	|  D  |   E  |     F	|
		// 					| gameID | date | notes | zap | draw | player 1 |

		gameID := fmt.Sprintf("%s", row[0])
		date := fmt.Sprintf("%s", row[1])
		zap := fmt.Sprintf("%s", row[2])
		draw := fmt.Sprintf("%s", row[3])

		g := &Game{
			ID:       gameID,
			Date:     date,
			Rankings: []string{},
			TableZap: zap,
			DrawGame: draw,
		}

		players := row[5:]

		for _, player := range players {
			name := fmt.Sprintf("%s", player)
			name = strings.Trim(name, " ")
			if strings.Contains(name, "/") {
				g.TwoHeadedGiant = true
				continue
			}
			g.Rankings = append(g.Rankings, name)
		}

		if g.TwoHeadedGiant {
			// TODO: Handle two headed giant scoring in the future.
			continue
		}
		games = append(games, g)
	}

	return games, nil
}

// calculateScores takes a slice of games and calculates their elo scores
// from default K and D values.
func calculateScores(games []*Game) map[string]int {
	elo := elogo.NewElo()
	scores := map[string]int{}

	for _, game := range games {
		if err := scoreGame(elo, scores, game); err != nil {
			log.Printf("failed to score game: %+v", err)
		}
	}

	if verbose {
		log.Printf("calculated scores: %+v", scores)
	}
	return scores
}

// scoreGame mutates a score map according to the provided elo values
// and adds the calculated values to the game
func scoreGame(elo *elogo.Elo, scores map[string]int, game *Game) error {
	numPlayers := len(game.Rankings)

	if numPlayers < 2 {
		return fmt.Errorf("invalid game: not enough players")
	}

	// determine rankings
	rankTotal := 0
	for _, player := range game.Rankings {
		_, ok := scores[player]
		if !ok {
			scores[player] = 1500
		}
		rankTotal += scores[player]
	}

	// calculate rank average
	rankAverage := rankTotal / numPlayers
	game.RankAverage = rankAverage
	game.RankTotal = rankTotal

	// assign rewards based on number of players
	updateScores(elo, scores, game)

	if verbose {
		log.Printf("scored game: %+v\n", game)
	}

	return nil
}

// updateScores updates the score map according to the approach
func updateScores(elo *elogo.Elo, scores map[string]int, game *Game) {
	for idx, player := range game.Rankings {
		var ratingsDelta int = 0
		var playerScore int = scores[player]

		switch {
		case len(game.Rankings) == 2:
			ratingsDelta = elo.RatingDelta(playerScore, game.RankAverage, twoPlayers[idx])
		case len(game.Rankings) == 3:
			ratingsDelta = elo.RatingDelta(playerScore, game.RankAverage, threePlayers[idx])
		case len(game.Rankings) == 4:
			ratingsDelta = elo.RatingDelta(playerScore, game.RankAverage, fourPlayers[idx])
		case len(game.Rankings) == 5:
			ratingsDelta = elo.RatingDelta(playerScore, game.RankAverage, fivePlayers[idx])
		case len(game.Rankings) == 6:
			ratingsDelta = elo.RatingDelta(playerScore, game.RankAverage, sixPlayers[idx])
		}

		if verbose {
			log.Printf("updating player ratings delta %d", ratingsDelta)
		}

		scores[player] += ratingsDelta
	}
}

func (g ByID) Len() int           { return len(g) }
func (g ByID) Less(i, j int) bool { return g[i].ID < g[j].ID }
func (g ByID) Swap(i, j int)      { g[i], g[j] = g[j], g[i] }

func (g ByScore) Len() int           { return len(g) }
func (g ByScore) Less(i, j int) bool { return g[i].Score > g[j].Score }
func (g ByScore) Swap(i, j int)      { g[i], g[j] = g[j], g[i] }
