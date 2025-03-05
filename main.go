package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v2"
)

// Config структура для загрузки конфигурации из YAML
type Config struct {
	InputDir     string `yaml:"inputDir"`
	OutputDir    string `yaml:"outputDir"`
	OCRAPIKey    string `yaml:"OCR_API_KEY"`
	GeminiAPIKey string `yaml:"GEMINI_API_KEY"`
}

var config Config

// OCR API Response Structure
type OCRResponse struct {
	ParsedResults []struct {
		ParsedText string `json:"ParsedText"`
	} `json:"ParsedResults"`
}

type GeminiRequest struct {
	Contents []Content `json:"contents"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

var processedFiles = make(map[string]bool)

// Функция загрузки конфигурации
func loadConfig() {
	data, err := ioutil.ReadFile("config.yml")
	if err != nil {
		log.Fatalf("Ошибка загрузки config.yml: %v", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		log.Fatalf("Ошибка разбора YAML: %v", err)
	}
}

func encodeImageToBase64(imagePath string) (string, error) {
	imageData, err := ioutil.ReadFile(imagePath)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(imageData), nil
}

func extractTextFromImage(imagePath string) (string, error) {
	imageBase64, err := encodeImageToBase64(imagePath)
	if err != nil {
		return "", err
	}

	client := resty.New()
	resp, err := client.R().
		SetHeader("apikey", config.OCRAPIKey).
		SetFormData(map[string]string{
			"language":                     "rus",
			"isOverlayRequired":            "false",
			"base64Image":                  "data:image/png;base64," + imageBase64,
			"iscreatesearchablepdf":        "false",
			"issearchablepdfhidetextlayer": "false",
		}).
		Post("https://api.ocr.space/parse/image")

	if err != nil {
		return "", err
	}

	var ocrResp OCRResponse
	if err := json.Unmarshal(resp.Body(), &ocrResp); err != nil {
		return "", err
	}

	if len(ocrResp.ParsedResults) > 0 {
		return ocrResp.ParsedResults[0].ParsedText, nil
	}

	return "", fmt.Errorf("no text found in image")
}

func getGeminiResponse(prompt string) (string, error) {
	client := resty.New()
	requestBody := GeminiRequest{
		Contents: []Content{{Parts: []Part{{Text: prompt}}}},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(bytes.NewBuffer(jsonData)).
		Post("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=" + config.GeminiAPIKey)

	if err != nil {
		return "", err
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(resp.Body(), &geminiResp); err != nil {
		return "", err
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		return geminiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("no response from Gemini API")
}

func saveToMarkdown(filename, content string) error {
	outputFilename := filepath.Join(config.OutputDir, filename+".md")
	err := ioutil.WriteFile(outputFilename, []byte(content), 0644)
	if err != nil {
		return err
	}

	fmt.Println("Файл сохранён:", outputFilename)
	return nil
}

func processFile(imagePath string) {
	fmt.Println("Обрабатывается файл:", imagePath)

	text, err := extractTextFromImage(imagePath)
	if err != nil {
		log.Printf("Ошибка OCR (%s): %v\n", imagePath, err)
		return
	}

	prompt := "Очень кратко объясни суть решения задачи и напиши код на GO:\n" + text
	response, err := getGeminiResponse(prompt)
	if err != nil {
		log.Printf("Ошибка Gemini API (%s): %v\n", imagePath, err)
		return
	}

	saveToMarkdown("result", response)
}

func watchDirectory() {
	for {
		files, err := ioutil.ReadDir(config.InputDir)
		if err != nil {
			log.Fatalf("Ошибка чтения директории %s: %v", config.InputDir, err)
		}

		for _, file := range files {
			if !file.IsDir() && !processedFiles[file.Name()] && (strings.HasSuffix(file.Name(), ".png") || strings.HasSuffix(file.Name(), ".jpg") || strings.HasSuffix(file.Name(), ".jpeg")) {
				processedFiles[file.Name()] = true
				processFile(filepath.Join(config.InputDir, file.Name()))
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	loadConfig()

	if _, err := os.Stat(config.OutputDir); os.IsNotExist(err) {
		os.Mkdir(config.OutputDir, os.ModePerm)
	}

	fmt.Println("Запуск мониторинга директории:", config.InputDir)
	watchDirectory()
}
