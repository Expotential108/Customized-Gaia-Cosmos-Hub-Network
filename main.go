// Copyright 2018 Goole Inc.
// Copyright 2020 Tobias Schwarz
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Binary explorer demo. Displays widgets for insights of blockchain behaviour.
// Exist when 'q' is pressed.
package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"log"
	"os"
	"os/signal"

	"gopkg.in/resty.v1"

	"github.com/jedib0t/go-pretty/table"
	"github.com/sacOO7/gowebsocket"
	"github.com/tidwall/gjson"

	"github.com/mum4k/termdash"
	"github.com/mum4k/termdash/cell"
	"github.com/mum4k/termdash/container"
	"github.com/mum4k/termdash/keyboard"
	"github.com/mum4k/termdash/linestyle"
	"github.com/mum4k/termdash/terminal/termbox"
	"github.com/mum4k/termdash/terminal/terminalapi"
	"github.com/mum4k/termdash/widgets/gauge"
	"github.com/mum4k/termdash/widgets/text"
)

const (
	appRPC        = "http://localhost"
	tendermintRPC = "https://rpc.cosmos.network/"
)

var givenPort = flag.String("p", "26657", "port to connect to as a string")

func main() {
	t, err := termbox.New()
	if err != nil {
		panic(err)
	}
	defer t.Close()

	flag.Parse()

	networkInfo := getFromRPC("status")
	networkStatus := gjson.Parse(networkInfo)

	ctx, cancel := context.WithCancel(context.Background())

	// Blocks parsing widget
	blocksWidget, err := text.New(text.RollContent(), text.WrapAtWords())
	if err != nil {
		panic(err)
	}
	if err := blocksWidget.Write("Latest block height " + networkStatus.Get("result.sync_info.latest_block_height").String() + "\n"); err != nil {
		panic(err)
	}

	// Transaction parsing widget
	transactionWidget, err := text.New(text.RollContent(), text.WrapAtWords())
	if err != nil {
		panic(err)
	}
	if err := transactionWidget.Write("Transactions will appear as soon as they are confirmed in a block.\n\n"); err != nil {
		panic(err)
	}

	validatorWidget, err := text.New(text.RollContent(), text.WrapAtWords())
	if err != nil {
		panic(err)
	}
	if err := validatorWidget.Write("List available validators.\n\n"); err != nil {
		panic(err)
	}

	peerWidget, err := text.New()
	if err != nil {
		panic(err)
	}
	if err := peerWidget.Write("0"); err != nil {
		panic(err)
	}

	healthWidget, err := text.New()
	if err != nil {
		panic(err)
	}
	if err := healthWidget.Write("🔴 no connection"); err != nil {
		panic(err)
	}

	timeWidget, err := text.New()
	if err != nil {
		panic(err)
	}
	currentTime := time.Now()
	if err := timeWidget.Write(fmt.Sprintf("%s\n", currentTime.Format("2006-01-02\n03:04:05 PM"))); err != nil {
		panic(err)
	}

	maxBlocksizeWidget, err := text.New()
	maxBlockSize := gjson.Get(getFromRPC("consensus_params"), "result.consensus_params.block.max_bytes").Int()
	if err != nil {
		panic(err)
	}
	if err := maxBlocksizeWidget.Write(fmt.Sprintf("%s", byteCountDecimal(maxBlockSize))); err != nil {
		panic(err)
	}

	// system powered widgets
	go writeTime(ctx, timeWidget, 1*time.Second)

	// websocket powered widgets
	go writeValidators(ctx, validatorWidget)
	go writeBlocks(ctx, blocksWidget)
	go writeTransactions(ctx, transactionWidget)

	// rpc widgets
	go writePeers(ctx, peerWidget, 1*time.Second)
	go writeHealth(ctx, healthWidget, 1*time.Second)

	// blockchain download gauge
	syncWidget, err := gauge.New(
		gauge.Height(1),
		gauge.Color(cell.ColorBlue),
		gauge.Border(linestyle.Light),
		gauge.BorderTitle("Blockchain download %"),
	)
	if err != nil {
		panic(err)
	}

	if networkStatus.Get("result.sync_info.catching_up").String() == "false" {
		if err := syncWidget.Absolute(100, 100); err != nil {
			panic(err)
		}
	} else {
		if networkStatus.Get("result.node_info.network").String() == "cosmoshub-3" {
			go syncGauge(ctx, syncWidget, networkStatus.Get("result.sync_info.latest_block_height").Int())
		} else {
			// There is no way to detect maximum height in the network via RPC or websocket yet
			if err := syncWidget.Absolute(70, 100); err != nil {
				panic(err)
			}
		}
	}

	// Draw Dashboard
	c, err := container.New(
		t,
		container.Border(linestyle.Light),
		container.BorderTitle("PRESS Q or ESC TO QUIT | Network "+networkStatus.Get("result.node_info.network").String()+" Version "+networkStatus.Get("result.node_info.version").String()),
		container.BorderColor(cell.ColorNumber(2)),
		container.SplitHorizontal(
			container.Top(
				container.SplitVertical(
					container.Left(
						container.SplitHorizontal(
							container.Top(
								container.SplitVertical(
									container.Left(
										container.SplitVertical(
											container.Left(
												container.Border(linestyle.Light),
												container.BorderTitle("Health"),
												container.PlaceWidget(healthWidget),
											),
											container.Right(
												container.Border(linestyle.Light),
												container.BorderTitle("System Time"),
												container.PlaceWidget(timeWidget),
											),
										),
									),
									container.Right(
										container.SplitVertical(
											container.Left(
												container.Border(linestyle.Light),
												container.BorderTitle("Max Block Size"),
												container.PlaceWidget(maxBlocksizeWidget),
											),
											container.Right(
												container.Border(linestyle.Light),
												container.BorderTitle("Connected Peers"),
												container.PlaceWidget(peerWidget),
											),
										),
									),
								),
							),
							container.Bottom(
								container.PlaceWidget(syncWidget),
							),
						),
					),
					container.Right(
						container.Border(linestyle.Light),
						container.BorderTitle("Validators"),
						container.PlaceWidget(validatorWidget),
					),
				),
			),
			container.Bottom(
				container.SplitVertical(
					container.Left(
						container.Border(linestyle.Light),
						container.BorderTitle("Latest Blocks"),
						container.PlaceWidget(blocksWidget),
					), container.Right(
						container.Border(linestyle.Light),
						container.BorderTitle("Latest Confirmed Transactions"),
						container.PlaceWidget(transactionWidget),
					),
				),
			),
		),
	)
	if err != nil {
		panic(err)
	}

	quitter := func(k *terminalapi.Keyboard) {
		if k.Key == 'q' || k.Key == 'Q' || k.Key == keyboard.KeyEsc {
			cancel()
		}
	}

	if err := termdash.Run(ctx, t, c, termdash.KeyboardSubscriber(quitter)); err != nil {
		panic(err)
	}
}

func getFromRPC(endpoint string) string {
	port := *givenPort
	resp, err := resty.R().
		SetHeader("Cache-Control", "no-cache").
		SetHeader("Content-Type", "application/json").
		Get(appRPC + ":" + port + "/" + endpoint)

	if err != nil {
		panic(err)
	}

	return resp.String()
}

func getTendermintRPC(endpoint string) string {
	resp, err := resty.R().
		SetHeader("Cache-Control", "no-cache").
		SetHeader("Content-Type", "application/json").
		Get(tendermintRPC + endpoint)

	if err != nil {
		panic(err)
	}

	return resp.String()
}

// writeTime writes the current system time to the timeWidget.
// Exits when the context expires.
func writeTime(ctx context.Context, t *text.Text, delay time.Duration) {
	ticker := time.NewTicker(delay)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			currentTime := time.Now()
			t.Reset()
			if err := t.Write(fmt.Sprintf("%s\n", currentTime.Format("2006-01-02\n03:04:05 PM"))); err != nil {
				panic(err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// writeHealth writes the status to the healthWidget.
// Exits when the context expires.
func writeHealth(ctx context.Context, t *text.Text, delay time.Duration) {
	health := gjson.Get(getFromRPC("health"), "result")
	t.Reset()
	if health.Exists() {
		if err := t.Write("🟢 good"); err != nil {
			panic(err)
		}
	} else {
		if err := t.Write("🔴 no connection"); err != nil {
			panic(err)
		}
	}

	ticker := time.NewTicker(delay)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			health := gjson.Get(getFromRPC("health"), "result")
			t.Reset()
			if health.Exists() {
				if err := t.Write("🟢 good"); err != nil {
					panic(err)
				}
			} else {
				if err := t.Write("🔴 no connection"); err != nil {
					panic(err)
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

// writePeers writes the connected Peers to the peerWidget.
// Exits when the context expires.
func writePeers(ctx context.Context, t *text.Text, delay time.Duration) {
	peers := gjson.Get(getFromRPC("net_info"), "result.n_peers").String()
	t.Reset()
	if err := t.Write(peers); err != nil {
		panic(err)
	}

	ticker := time.NewTicker(delay)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.Reset()
			peers := gjson.Get(getFromRPC("net_info"), "result.n_peers").String()
			if err := t.Write(peers); err != nil {
				panic(err)
			}

		case <-ctx.Done():
			return
		}
	}
}

// writeTransactions writes the latest Transactions to the transactionsWidget.
// Exits when the context expires.
func writeTransactions(ctx context.Context, t *text.Text) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	socket := gowebsocket.New("ws://localhost:26657/websocket")

	socket.OnConnectError = func(err error, socket gowebsocket.Socket) {
		log.Fatal("Received connect error - ", err)
	}
	socket.OnTextMessage = func(message string, socket gowebsocket.Socket) {
		currentTx := gjson.Get(message, "result.data.value.TxResult.result.log")
		currentTime := time.Now()
		if currentTx.String() != "" {
			if err := t.Write(fmt.Sprintf("%s\n", currentTime.Format("2006-01-02 03:04:05 PM")+"\n"+currentTx.String())); err != nil {
				panic(err)
			}
		}
	}

	socket.OnDisconnected = func(err error, socket gowebsocket.Socket) {
		log.Println("Disconnected from server ")
		return
	}

	socket.Connect()

	socket.SendText("{ \"jsonrpc\": \"2.0\", \"method\": \"subscribe\", \"params\": [\"tm.event='Tx'\"], \"id\": 2 }")

	for {
		select {
		case <-ctx.Done():
			log.Println("interrupt")
			socket.Close()
			return
		}
	}
}

// writeBlocks writes the latest Block to the blocksWidget.
// Exits when the context expires.
func writeBlocks(ctx context.Context, t *text.Text) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	socket := gowebsocket.New("ws://localhost:26657/websocket")

	socket.OnConnectError = func(err error, socket gowebsocket.Socket) {
		log.Fatal("Received connect error - ", err)
	}

	socket.OnTextMessage = func(message string, socket gowebsocket.Socket) {
		currentBlock := gjson.Get(message, "result.data.value.block.header.height")
		if currentBlock.String() != "" {
			if err := t.Write(fmt.Sprintf("%s\n", "Latest block height "+currentBlock.String())); err != nil {
				panic(err)
			}
		}

	}

	socket.OnDisconnected = func(err error, socket gowebsocket.Socket) {
		log.Println("Disconnected from server ")
		return
	}

	socket.Connect()

	socket.SendText("{ \"jsonrpc\": \"2.0\", \"method\": \"subscribe\", \"params\": [\"tm.event='NewBlock'\"], \"id\": 1 }")

	for {
		select {
		case <-ctx.Done():
			log.Println("interrupt")
			socket.Close()
			return
		}
	}
}

// writeValidators writes the current validator set to the validatoWidget
// Exits when the context expires.
func writeValidators(ctx context.Context, t *text.Text) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	socket := gowebsocket.New("ws://localhost:26657/websocket")

	socket.OnConnectError = func(err error, socket gowebsocket.Socket) {
		log.Fatal("Received connect error - ", err)
	}

	socket.OnConnected = func(socket gowebsocket.Socket) {
		validators := gjson.Get(getFromRPC("validators"), "result.validators")
		t.Reset()
		i := 1
		validators.ForEach(func(key, validator gjson.Result) bool {

			ta := table.NewWriter()
			ta.AppendRow([]interface{}{fmt.Sprintf("%d", i), validator.Get("address").String(), validator.Get("voting_power").String()})

			if err := t.Write(fmt.Sprintf("%s\n", ta.Render())); err != nil {
				panic(err)
			}
			i++
			return true // keep iterating
		})
	}

	socket.OnTextMessage = func(message string, socket gowebsocket.Socket) {
		validators := gjson.Get(getFromRPC("validators"), "result.validators")
		t.Reset()

		i := 1
		validators.ForEach(func(key, validator gjson.Result) bool {

			ta := table.NewWriter()
			ta.AppendRow([]interface{}{fmt.Sprintf("%d", i), validator.Get("address").String(), validator.Get("voting_power").String()})

			if err := t.Write(fmt.Sprintf("%s\n", ta.Render())); err != nil {
				panic(err)
			}
			i++
			return true // keep iterating
		})
	}

	socket.OnDisconnected = func(err error, socket gowebsocket.Socket) {
		log.Println("Disconnected from server ")
		return
	}

	socket.Connect()

	socket.SendText("{ \"jsonrpc\": \"2.0\", \"method\": \"subscribe\", \"params\": [\"tm.event='ValidatorSetUpdates'\"], \"id\": 3 }")

	for {
		select {
		case <-ctx.Done():
			log.Println("interrupt")
			socket.Close()
			return
		}
	}
}

// syncGauge displays the syncing status in the syncWidget
// Exits when the context expires.
func syncGauge(ctx context.Context, g *gauge.Gauge, blockHeight int64) {
	var progress int64 = 0

	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:

			maxHeight := gjson.Get(getTendermintRPC("dump_consensus_state"), "result.round_state.height").Int()

			progress = (blockHeight / maxHeight) * 100
			if err := g.Absolute(int(progress), 100); err != nil {
				panic(err)
			}

		case <-ctx.Done():
			return
		}
	}
}

// byteCountDecimal calculates bytes integer to a human readable decimal number
func byteCountDecimal(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}
