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
	"github.com/joho/godotenv"
)

// OCR API Response Structure
type OCRResponse struct {
	ParsedResults []struct {
		ParsedText string `json:"ParsedText"`
	} `json:"ParsedResults"`
}

// Структура запроса для Gemini API
type GeminiRequest struct {
	Contents []Content `json:"contents"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

// Структура ответа от Gemini API
type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Глобальные настройки
const inputDir = "/Users/hq-qd17x1gc43/Yandex.Disk.localized/Скриншоты" // Директория для отслеживания файлов
// const inputDir = "./input"
const outputDir = "/Users/hq-qd17x1gc43/Yandex.Disk.localized/Obsidian Vault YA/output" // Директория для сохранения Markdown-файлов

// Множество уже обработанных файлов
var processedFiles = make(map[string]bool)

// Функция загрузки переменных окружения
func loadEnv() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}
}

// Функция кодирования изображения в base64
func encodeImageToBase64(imagePath string) (string, error) {
	imageData, err := ioutil.ReadFile(imagePath)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(imageData), nil
}

// Функция отправки изображения в OCR API
func extractTextFromImage(imagePath string) (string, error) {
	imageBase64, err := encodeImageToBase64(imagePath)
	if err != nil {
		return "", err
	}

	client := resty.New()
	resp, err := client.R().
		SetHeader("apikey", os.Getenv("OCR_API_KEY")).
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
	err = json.Unmarshal(resp.Body(), &ocrResp)
	if err != nil {
		return "", err
	}

	if len(ocrResp.ParsedResults) > 0 {
		return ocrResp.ParsedResults[0].ParsedText, nil
	}

	return "", fmt.Errorf("no text found in image")
}

// Функция отправки запроса в Gemini API
func getGeminiResponse(prompt string) (string, error) {
	client := resty.New()

	requestBody := GeminiRequest{
		Contents: []Content{
			{
				Parts: []Part{
					{Text: prompt},
				},
			},
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(bytes.NewBuffer(jsonData)).
		Post("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=" + os.Getenv("GEMINI_API_KEY"))

	if err != nil {
		return "", err
	}

	var geminiResp GeminiResponse
	err = json.Unmarshal(resp.Body(), &geminiResp)
	if err != nil {
		return "", err
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		return geminiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("no response from Gemini API")
}

// Функция сохранения ответа в Markdown-файл
func saveToMarkdown(filename, content string) error {
	// Генерируем имя выходного файла
	outputFilename := filepath.Join(outputDir, filename+".md")

	// Оформляем Markdown
	//mdContent := fmt.Sprintf("# Ответ Gemini API\n\n```\n%s\n```", content)

	// Записываем в файл
	err := ioutil.WriteFile(outputFilename, []byte(content), 0644)
	if err != nil {
		return err
	}

	fmt.Println("Файл сохранён:", outputFilename)
	return nil
}

// Функция обработки нового файла
func processFile(imagePath string) {
	fmt.Println("Обрабатывается файл:", imagePath)

	// Распознаем текст
	text, err := extractTextFromImage(imagePath)
	if err != nil {
		log.Printf("Ошибка OCR (%s): %v\n", imagePath, err)
		return
	}

	// Отправляем текст в Gemini
	prompt := "Очень кратко объясни суть решения задачи и напиши код на GO:\n" + text
	response, err := getGeminiResponse(prompt)
	if err != nil {
		log.Printf("Ошибка Gemini API (%s): %v\n", imagePath, err)
		return
	}

	// Сохраняем результат
	//filename := strings.TrimSuffix(filepath.Base(imagePath), filepath.Ext(imagePath))
	filename := "result"
	err = saveToMarkdown(filename, response)
	if err != nil {
		log.Printf("Ошибка сохранения файла (%s): %v\n", filename, err)
	}
}

// Функция мониторинга директории
func watchDirectory() {
	for {
		files, err := ioutil.ReadDir(inputDir)
		if err != nil {
			log.Fatalf("Ошибка чтения директории %s: %v", inputDir, err)
		}

		for _, file := range files {
			// Проверяем, что это новый файл и он является изображением
			if !file.IsDir() && !processedFiles[file.Name()] && (strings.HasSuffix(file.Name(), ".png") || strings.HasSuffix(file.Name(), ".jpg") || strings.HasSuffix(file.Name(), ".jpeg")) {
				processedFiles[file.Name()] = true // Помечаем файл как обработанный
				processFile(filepath.Join(inputDir, file.Name()))
			}
		}

		time.Sleep(100 * time.Millisecond) // Задержка перед следующей проверкой
	}
}

// Главная функция
func main() {
	loadEnv()

	// Проверяем, существует ли директория output
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		os.Mkdir(outputDir, os.ModePerm)
	}

	fmt.Println("Запуск мониторинга директории:", inputDir)
	watchDirectory() // Запускаем бесконечный цикл проверки файлов
}
