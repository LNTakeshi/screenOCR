package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/sashabaranov/go-openai"
	"gopkg.in/yaml.v3"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

type config struct {
	OpenAIKey string `yaml:"openai_key"`
	WatchDir  string `yaml:"watch_dir"`
}

var cfg config
var cli *openai.Client

func main() {
	defer func() {
		time.Sleep(2 * time.Second)
	}()
	configFilePath := flag.String("config", "config.yaml", "config file path")
	flag.Parse()

	cli = openai.NewClient(cfg.OpenAIKey)

	cfgFile, err := os.ReadFile(*configFilePath)
	if err != nil {
		log.Panicln(err)
	}

	if err = yaml.Unmarshal(cfgFile, &cfg); err != nil {
		log.Panicln(err)
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Panicln(err)
	}
	defer watcher.Close()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Has(fsnotify.Write) {
					ocr(event.Name)
				}
			case err2, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err2)
			}
		}
	}()

	err = watcher.Add(cfg.WatchDir)
	if err != nil {
		log.Panicln(err)
	}

	fmt.Println("ディレクトリ: " + cfg.WatchDir + "監視開始")

	// Block main goroutine forever.
	<-make(chan struct{})
}

func ocr(filename string) {
	if !strings.Contains(filename, "png") || strings.Contains(filename, "jpg") {
		return
	}
	f, err := os.OpenFile(filename, os.O_RDONLY, 0666)
	if err != nil {
		log.Println(err)
		return
	}
	s, err := f.Stat()
	if err != nil {
		log.Println(err)
		return
	}
	if s.Size() == 0 {
		return
	}
	img, err := png.Decode(f)
	if err != nil {
		log.Println(err)
		return
	}
	imgbuf := &bytes.Buffer{}
	err = jpeg.Encode(imgbuf, img, nil)
	if err != nil {
		log.Println(err)
		return
	}

	buf := &bytes.Buffer{}
	e := base64.NewEncoder(base64.StdEncoding, buf)
	_, err = io.Copy(e, imgbuf)
	if err != nil {
		log.Println(err)
		return
	}

	if err = e.Close(); err != nil {
		log.Println(err)
		return
	}

	fmt.Println(filename + "読み取り中")

	resp, err := cli.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT4o,
			Messages: []openai.ChatCompletionMessage{
				{
					Role: openai.ChatMessageRoleUser,
					MultiContent: []openai.ChatMessagePart{
						{
							Type: openai.ChatMessagePartTypeText,
							Text: "次の画像を日本語に翻訳し、翻訳結果を表示してください。",
						},
						{
							Type: openai.ChatMessagePartTypeImageURL,
							ImageURL: &openai.ChatMessageImageURL{
								URL:    "data:image/jpeg;base64," + buf.String(),
								Detail: openai.ImageURLDetailAuto,
							},
						},
					},
				},
			},
		},
	)
	if err != nil {
		log.Println(err)
		return
	}
	fmt.Println(resp.Choices[0].Message.Content)
	fmt.Printf("prompt token: %d, completion token: %d, total token: %d\n", resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}
