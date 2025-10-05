package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

type API struct {
	DB *dynamodb.Client
	S3 *s3.Client
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

func initS3() *s3.Client {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("unable to load SDK config: %v", err)
	}
	s3Clent := s3.NewFromConfig(cfg)
	return s3Clent
}

func main() {
	initEnv()

	api := &API{
		DB: initDB(),
		S3: initS3(),
	}

	router := gin.Default()

	router.GET("/ping", ping)
	router.GET("/missions", api.getMissions)
	router.GET("/images/:id", api.getSatImageByID)

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

func (api *API) getSatImageByID(c *gin.Context) {
	bucketName := os.Getenv("SAT_IMAGES_BUCKET")
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}

	key := fmt.Sprintf("%s.jpg", id)

	in := &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}
	if rng := c.GetHeader("Range"); rng != "" {
		in.Range = aws.String(rng)
	}

	out, err := api.S3.GetObject(c.Request.Context(), in)
	if err != nil {
		log.Printf("s3 GetObject error key=%s: %v", key, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "object not found"})
		return
	}
	defer out.Body.Close()

	if out.ContentType != nil {
		c.Header("Content-Type", aws.ToString(out.ContentType))
	}
	if out.ContentLength != nil {
		c.Header("Content-Length", strconv.FormatInt(*out.ContentLength, 10))
	}
	if out.ETag != nil {
		c.Header("ETag", aws.ToString(out.ETag))
	}
	if out.LastModified != nil {
		c.Header("Last-Modified", out.LastModified.UTC().Format(http.TimeFormat))
	}
	if out.CacheControl != nil {
		c.Header("Cache-Control", aws.ToString(out.CacheControl))
	} else {
		c.Header("Cache-Control", "private, max-age=60")
	}
	c.Header("Accept-Ranges", "bytes")
	if out.ContentRange != nil {
		c.Header("Content-Range", aws.ToString(out.ContentRange))
		c.Status(http.StatusPartialContent)
	} else {
		c.Status(http.StatusOK)
	}

	if _, err := io.Copy(c.Writer, out.Body); err != nil {
		log.Printf("error streaming key=%s: %v", key, err)
	}
}
