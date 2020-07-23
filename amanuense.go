package amanuense

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	speech "cloud.google.com/go/speech/apiv1"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"
)

type transcription struct {
	text       string
	confidence float32
	duration   int
}

var token = os.Getenv("BOT_TOKEN")

const (
	startMessage = "Ciao, sono Amanuense. Inoltrami un messaggio audio da trascrivere."
	aboutMessage = "Creato da @emilioschepis\n[Codice disponibile su GitHub](https://github.com/emilioschepis/amanuense-go)"
	errorMessage = "C'è stato un errore durante la trascrizione. Riprova più tardi."
)

// HandleMessage receives a Telegram message and parses the voice file content
// using Google's Speech-To-Text technology.
func HandleMessage(w http.ResponseWriter, r *http.Request) {
	if token == "" {
		log.Printf("no bot token available")
		return
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Printf("could not create telegram bot: %v\n", err)
		return
	}

	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("could not read request: %v\n", err)
		return
	}

	var update tgbotapi.Update

	if err := json.Unmarshal(data, &update); err != nil {
		log.Printf("could not parse request: %v\n", err)
		return
	}

	cid := update.Message.Chat.ID

	switch {
	case update.Message.Voice != nil:
		handleVoiceMessage(bot, cid, update.Message.Voice)
	case update.Message.Text != "":
		handleTextMessage(bot, cid, update.Message.Text)
	}
}

// extractSampleRate reads bytes from an OGG/OPUS file and explores relevant
// bytes to extract the sample rate of the file.
// https://wiki.xiph.org/OggOpus#ID_Header
func extractSampleRate(data []byte) uint32 {
	// Skip 40 bits (28 of the OGG header + 12 of the Opus header) and read the
	// next 4 bits to extract the sample rate as an int.
	if len(data) < 44 {
		log.Panicf("unexpected data length: %v", len(data))
	}

	return binary.LittleEndian.Uint32(data[40:44])
}

func handleVoiceMessage(bot *tgbotapi.BotAPI, chatID int64, voice *tgbotapi.Voice) {
	if voice.Duration > 60 {
		sendMessage(bot, chatID, "Trascrivo solo messaggi fino a 60 secondi!")
		return
	}

	sendMessage(bot, chatID, "Trascrizione in corso...")

	d, err := downloadVoiceMessage(bot, voice)
	if err != nil {
		log.Printf("could not get voice data: %v\n", err)
		sendMessage(bot, chatID, errorMessage)
		return
	}

	t, err := transcribeVoice(d)
	if err != nil {
		log.Printf("could not transcribe voice: %v\n", err)
		sendMessage(bot, chatID, errorMessage)
		return
	}
	if t == nil {
		sendMessage(bot, chatID, "Questo messaggio sembra essere vuoto.")
		return
	}

	text := t.text + fmt.Sprintf("\n\n\\[confidenza: %.1f%%; durata: %d secondi]", t.confidence*100, t.duration)
	sendMessage(bot, chatID, text)
}

func handleTextMessage(bot *tgbotapi.BotAPI, chatID int64, text string) {
	switch text {
	case "/start":
		sendMessage(bot, chatID, startMessage)
	case "/about":
		sendMessage(bot, chatID, aboutMessage)
	}
}

func transcribeVoice(data []byte) (*transcription, error) {
	// TODO: Understand the `context` concept.
	ctx := context.Background()

	sr := extractSampleRate(data)

	sc, err := speech.NewClient(ctx)
	if err != nil {
		log.Printf("could not instantiate speech client: %v\n", err)
		return nil, err
	}
	defer sc.Close()

	config := &speechpb.RecognitionConfig{
		Encoding:        speechpb.RecognitionConfig_OGG_OPUS,
		SampleRateHertz: int32(sr),
		LanguageCode:    "it-IT",
	}

	audio := &speechpb.RecognitionAudio{
		AudioSource: &speechpb.RecognitionAudio_Content{
			Content: data,
		},
	}

	req := &speechpb.RecognizeRequest{
		Config: config,
		Audio:  audio,
	}

	start := time.Now()
	res, err := sc.Recognize(ctx, req)
	if err != nil {
		log.Printf("could not recognize audio: %v\n", err)
		return nil, err
	}
	duration := time.Since(start)

	if len(res.Results) == 0 {
		return nil, nil
	}

	parts := make([]string, 0, len(res.Results))
	var conf float32

	for _, r := range res.Results {
		if len(r.Alternatives) == 0 {
			continue
		}
		parts = append(parts, r.Alternatives[0].Transcript)
		conf += r.Alternatives[0].Confidence
	}

	return &transcription{
		text:       strings.Join(parts, " "),
		confidence: conf / float32(len(res.Results)),
		duration:   int(duration.Seconds()),
	}, nil
}

func downloadVoiceMessage(bot *tgbotapi.BotAPI, voice *tgbotapi.Voice) ([]byte, error) {
	url, err := bot.GetFileDirectURL(voice.FileID)
	if err != nil {
		log.Printf("could not get voice file url: %v\n", err)
		return nil, err
	}

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("could not download voice file: %v\n", err)
		return nil, err
	}

	return ioutil.ReadAll(resp.Body)
}

func sendMessage(bot *tgbotapi.BotAPI, chatID int64, message string) {
	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = "markdown"
	bot.Send(msg)
}
