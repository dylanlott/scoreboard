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
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	elogo "github.com/kortemy/elo-go"
)

//go:embed templates/*
var resources embed.FS

var t = template.Must(template.ParseFS(resources, "templates/*"))

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"

	}

	// - [x] Auth with Google API
	// - [x] Load google sheets data
	// - [ ] Run the elo score calculations on the current history
	// - [ ] Show the current elo scores

	fetchGameData()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		games, err := fetchGameData()
		if err != nil {
			log.Fatalf("failed to load game data: %+v", err)
		}
		g, err := json.Marshal(games)
		if err != nil {
			log.Fatalf("failed to marshal data: %+v", err)
		}
		data := map[string]string{
			"version":      "0.0.1",
			"last_updated": time.Now().String(),
			"games":        string(g),
			"total":        fmt.Sprintf("%d", len(games)),
		}

		t.ExecuteTemplate(w, "index.html.tmpl", data)
	})

	log.Println("listening on", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
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

// Game is a modeled MTG Game with a set of rankings determined by order of player loss.
type Game struct {
	ID       string
	Date     string
	Rankings []Player
	TableZap string // if table zap is marked, then first place is the winner and the others lose equally, instead of in order as usual
	DrawGame string // if draw game is marked, the game ended in a draw for all players, so order doesn't matter but players still need to be recorded.
}

type Player string

func fetchGameData() ([]Game, error) {
	ctx := context.Background()
	b, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, "https://www.googleapis.com/auth/spreadsheets.readonly")
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Sheets client: %v", err)
	}

	// NOTE: spreadsheetId for the game tracker
	spreadsheetId := "1-qr-ejHx07Hrr35OymMcGRH00-Jzb-k8S8-xS9P5vqk"

	// CSV Schema
	//
	// Each row is a game in the data.
	// Columns C through H map to in-order player rankings for a given game.
	// This schema supports up to 6 players.
	//
	// column schema:  	|   A	 | 	B 	|   C  	|  D  |   E  |     F	| ...........  |
	// 					| gameID | date | notes | zap | draw | player 1 | ... player n |
	readRange := "Ranked game log!A:K"
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetId, readRange).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve data from sheet: %w", err)
	}
	if len(resp.Values) == 0 {
		return nil, fmt.Errorf("no game data found")
	} else {
		games, err := parseGameData(resp.Values)
		if err != nil {
			return nil, err
		}
		return games, nil
	}
}

func parseGameData(values [][]interface{}) ([]Game, error) {
	var games []Game
	for idx, row := range values {
		if idx == 0 {
			// skip the first row, it's only labels
			continue
		}

		gameID := fmt.Sprintf("%d", row[0])
		date := fmt.Sprintf("%s", row[1])
		zap := fmt.Sprintf("%s", row[2])
		draw := fmt.Sprintf("%s", row[3])

		g := Game{
			ID:       gameID,
			Date:     date,
			Rankings: []Player{},
			TableZap: zap,
			DrawGame: draw,
		}

		players := row[5:]

		for _, player := range players {
			g.Rankings = append(g.Rankings, Player(fmt.Sprintf("%s", player)))
		}

		games = append(games, g)
	}
	return games, nil
}

func calculateScores(games []Game) map[string]int {
	elo := elogo.NewElo()
	elo.D = 800
	elo.K = 40

	scores := map[string]int{}

	for idx, game := range games {
		players := game.Rankings

		fmt.Printf("# gameID: %v\n", idx)

		for _, player := range players {
			name := strings.Trim(string(player), " ")
			if score, ok := scores[name]; !ok {
				scores[name] = 1500
			} else {
				fmt.Printf("v: %v\n", score)
			}
		}
	}

	return scores
}
