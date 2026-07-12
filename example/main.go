// Command example is a minimal whatsmeow bot wired end-to-end with whatsmeow-wam:
// it uploads the WAM (Falco) telemetry batches WA Web sends, auto-emits protocol
// and integrator events, and fabricates plausible synthetic UI activity.
//
// Run:
//
//	go run .            # normal
//	go run . --debug    # also log every WAM batch upload
//
// On first run it prints a QR code — scan it from WhatsApp > Linked Devices.
// Send "ping" to the linked account from another phone and it replies "pong",
// demonstrating OnSendMessage (the content-level WAM events).
//
// Storage uses the pure-Go modernc.org/sqlite driver (no cgo). To use the
// canonical cgo driver instead, import _ "github.com/mattn/go-sqlite3" and change
// the dialect to "sqlite3" and the DSN to "file:whatsmeow-wam.db?_foreign_keys=on".
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	wam "github.com/zennn08/whatsmeow-wam"
	"github.com/zennn08/whatsmeow-wam/wmtransport"

	_ "modernc.org/sqlite"
)

func main() {
	debug := flag.Bool("debug", false, "log every WAM batch upload (and drops/failures)")
	flag.Parse()

	ctx := context.Background()

	// 1. Store + device.
	//    Open the DB ourselves so we can serialize writes: whatsmeow writes the
	//    store from many goroutines during history sync, and SQLite returns
	//    SQLITE_BUSY ("database is locked") under concurrent writers. MaxOpenConns(1)
	//    forces one connection (writes queue instead of failing); WAL + busy_timeout
	//    smooth over the rest. Without this, the initial sync corrupts app-state
	//    (mismatching LTHash) because mid-write saves fail.
	db, err := sql.Open("sqlite", "file:whatsmeow-wam.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		panic(err)
	}
	db.SetMaxOpenConns(1)
	container := sqlstore.NewWithDB(db, "sqlite", waLog.Stdout("DB", "INFO", true))
	if err := container.Upgrade(ctx); err != nil {
		panic(err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		panic(err)
	}

	// 2. Client.
	cli := whatsmeow.NewClient(device, waLog.Stdout("Client", "INFO", true))

	// 3. Wire whatsmeow-wam.
	//    NewCoordinator sets up the w:stats uploader + globals from the device.
	//    With --debug, wrap the uploader so every batch is logged (WAM is silent
	//    by default, so this is how you confirm telemetry is actually going out).
	opts := wam.Options{}
	if *debug {
		opts.Transport = logTransport{wmtransport.New(cli.DangerousInternals())}
		opts.Logf = func(f string, a ...any) { fmt.Printf("[wam] "+f+"\n", a...) }
	}
	coord := wmtransport.NewCoordinator(cli, opts)
	//    AttachAutoEmitter registers the typed + raw-node handlers (Phase 2).
	ae := wmtransport.AttachAutoEmitter(cli, coord)
	//    Opt-in integrator actions (app-state; see the README caveat).
	ae.EmitAppStateActions = true
	//    Opt-in synthetic UI telemetry (Phase 3). Tune or gate as needed, e.g.
	//    confine to waking hours or enable channel/community/business surfaces.
	synth := ae.StartSynthetic(wmtransport.SyntheticOptions{
		// ActiveHoursStartHour: ptr(8), ActiveHoursEndHour: ptr(23),
		// Channels: true, Communities: true, Business: true,
	})

	// 4. Demo handler: reply "pong" to "ping", routing the send through WAM so the
	//    content-level events (here just a text send) are captured.
	cli.AddEventHandler(func(evt any) {
		m, ok := evt.(*events.Message)
		if !ok || m.Info.IsFromMe {
			return
		}
		text := m.Message.GetConversation()
		if text == "" {
			text = m.Message.GetExtendedTextMessage().GetText()
		}
		if strings.EqualFold(strings.TrimSpace(text), "ping") {
			reply := &waE2E.Message{Conversation: proto.String("pong")}
			ae.OnSendMessage(m.Info.Chat, reply) // WAM telemetry — call BEFORE the send
			if _, err := cli.SendMessage(ctx, m.Info.Chat, reply); err != nil {
				fmt.Println("send failed:", err)
			}
		}
	})

	// 5. Connect (QR login on first run).
	if cli.Store.ID == nil {
		qrChan, err := cli.GetQRChannel(ctx)
		if err != nil {
			panic(err)
		}
		if err := cli.Connect(); err != nil {
			panic(err)
		}
		for item := range qrChan {
			if item.Event == whatsmeow.QRChannelEventCode {
				fmt.Println("Scan this QR from WhatsApp > Linked Devices:")
				qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, os.Stdout)
			} else {
				fmt.Println("Login event:", item.Event)
			}
		}
	} else {
		if err := cli.Connect(); err != nil {
			panic(err)
		}
	}
	fmt.Println("Connected. WAM telemetry active. Send 'ping' to test. Ctrl-C to quit.")

	// 6. Wait for Ctrl-C, then flush WAM and disconnect cleanly.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	synth.Stop()       // stop fabricating
	coord.Close(ctx)   // emit WebWamForceFlush + flush remaining batches
	cli.Disconnect()
	fmt.Println("Bye.")
}

// logTransport wraps the real WAM uploader to print each batch (used by --debug).
type logTransport struct{ inner wam.Transport }

func (t logTransport) Upload(ctx context.Context, batch []byte) error {
	err := t.inner.Upload(ctx, batch)
	if err != nil {
		fmt.Printf("[wam] upload FAILED (%d bytes): %v\n", len(batch), err)
	} else {
		fmt.Printf("[wam] uploaded %d bytes\n", len(batch))
	}
	return err
}

//nolint:unused // handy when enabling ActiveHours in SyntheticOptions above.
func ptr(v int) *int { return &v }
