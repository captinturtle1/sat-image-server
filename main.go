package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

type API struct {
	DB *dynamodb.Client
}

type Mission struct {
	ID        string   `dynamodbav:"id" json:"id"`
	Name      string   `dynamodbav:"name" json:"name"`
	StartDate int64    `dynamodbav:"start_date" json:"start_date"`
	Active    bool     `dynamodbav:"active" json:"active"`
	ImageIDs  []string `dynamodbav:"image_ids" json:"image_ids"`
}

func initEnv() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
}

func initDB() *dynamodb.Client {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	dbClient := dynamodb.NewFromConfig(cfg)
	return dbClient
}

func main() {
	initEnv()

	api := &API{
		DB: initDB(),
	}

	router := gin.Default()

	router.GET("/ping", ping)
	router.GET("/missions", api.getMissions)

	router.Run("localhost:8080")

}

func ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}

func (api *API) getMissions(c *gin.Context) {
	tableName := os.Getenv("MISSION_TABLE")

	println(tableName)

	paginator := dynamodb.NewScanPaginator(api.DB, &dynamodb.ScanInput{
		TableName: aws.String(tableName),
	})

	var allItems []Mission

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve missions"})
			return
		}

		var pagedItems []Mission

		err = attributevalue.UnmarshalListOfMaps(page.Items, &pagedItems)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve missions"})
			return
		}

		allItems = append(allItems, pagedItems...)
	}

	c.IndentedJSON(http.StatusOK, allItems)
}
