package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/nfnt/resize"
)

type API struct {
	DB *dynamodb.Client
	S3 *s3.Client
}

type Mission struct {
	ID                    string   `dynamodbav:"id" json:"id"`
	Name                  string   `dynamodbav:"name" json:"name"`
	Status                string   `dynamodbav:"status" json:"status"`
	Priority              int      `dynamodbav:"priority" json:"priority"`
	TargetSatelliteID     string   `dynamodbav:"target_satellite_id" json:"target_satellite_id"`
	ObserverSatelliteID   string   `dynamodbav:"observer_satellite_id" json:"observer_satellite_id"`
	TCA                   int64    `dynamodbav:"tca" json:"tca"`
	MinRangeKM            float64  `dynamodbav:"min_range_km" json:"min_range_km"`
	CollectionWindowStart int64    `dynamodbav:"collection_window_start" json:"collection_window_start"`
	CollectionWindowEnd   int64    `dynamodbav:"collection_window_end" json:"collection_window_end"`
	CollectionType        string   `dynamodbav:"collection_type" json:"collection_type"`
	PointingTarget        string   `dynamodbav:"pointing_target" json:"pointing_target"`
	ImageIDs              []string `dynamodbav:"image_ids" json:"image_ids"`
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
	api := &API{
		DB: initDB(),
		S3: initS3(),
	}

	router := gin.Default()

	router.GET("/ping", ping)
	router.GET("/missions", api.getMissions)
	router.GET("/mission/:id", api.getMissionById)
	router.GET("/image/:id", api.getSatImageByID)

	router.Run(":8080")
}

func ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}

type PaginatedMissionsResponse struct {
	Missions  []Mission `json:"missions"`
	NextToken *string   `json:"nextToken,omitempty"`
}

func (api *API) getMissions(c *gin.Context) {
	tableName := os.Getenv("MISSION_TABLE")
	limit := int32(100)

	scanInput := &dynamodb.ScanInput{
		TableName: aws.String(tableName),
		Limit:     aws.Int32(limit),
	}

	token := c.Query("nextToken")
	if token != "" {
		decodedToken, err := base64.StdEncoding.DecodeString(token)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid pagination token"})
			return
		}

		var exclusiveStartKey map[string]types.AttributeValue
		if err := json.Unmarshal(decodedToken, &exclusiveStartKey); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid pagination token format"})
			return
		}
		scanInput.ExclusiveStartKey = exclusiveStartKey
	}

	output, err := api.DB.Scan(c.Request.Context(), scanInput)
	if err != nil {
		log.Printf("DynamoDB scan failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve missions"})
		return
	}

	var missions []Mission
	err = attributevalue.UnmarshalListOfMaps(output.Items, &missions)
	if err != nil {
		log.Printf("Failed to unmarshal missions: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process mission data"})
		return
	}

	var nextToken *string
	if len(output.LastEvaluatedKey) > 0 {
		jsonKey, err := json.Marshal(output.LastEvaluatedKey)
		if err != nil {
			log.Printf("Failed to marshal LastEvaluatedKey: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare pagination token"})
			return
		}
		encodedToken := base64.StdEncoding.EncodeToString(jsonKey)
		nextToken = aws.String(encodedToken)
	}

	response := PaginatedMissionsResponse{
		Missions:  missions,
		NextToken: nextToken,
	}

	c.IndentedJSON(http.StatusOK, response)
}

func (api *API) getMissionById(c *gin.Context) {
	tableName := os.Getenv("MISSION_TABLE")

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}

	out, err := api.DB.GetItem(c.Request.Context(), &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve mission"})
		return
	}
	if out.Item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mission not found"})
		return
	}
	var mission Mission
	err = attributevalue.UnmarshalMap(out.Item, &mission)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve mission"})
		return
	}
	c.IndentedJSON(http.StatusOK, mission)
}

func encodeImage(w io.Writer, img image.Image, format string) error {
	switch format {
	case "jpeg":
		return jpeg.Encode(w, img, nil)
	case "png":
		return png.Encode(w, img)
	default:
		return fmt.Errorf("unsupported image format: %s", format)
	}
}

func (api *API) getSatImageByID(c *gin.Context) {
	bucketName := os.Getenv("SAT_IMAGES_BUCKET")
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}

	key := fmt.Sprintf("images/%s.jpg", id)

	widthStr := c.Query("width")
	heightStr := c.Query("height")
	width, _ := strconv.Atoi(widthStr)
	height, _ := strconv.Atoi(heightStr)

	in := &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}

	if width <= 0 && height <= 0 {
		if rng := c.GetHeader("Range"); rng != "" {
			in.Range = aws.String(rng)
		}
	}

	out, err := api.S3.GetObject(c.Request.Context(), in)
	if err != nil {
		log.Printf("s3 GetObject error key=%s: %v", key, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "object not found"})
		return
	}
	defer out.Body.Close()

	// if width and height are specified, resize the image, otherwise stream it directly for performance
	if width > 0 && height > 0 {
		img, format, err := image.Decode(out.Body)
		if err != nil {
			log.Printf("failed to decode image key=%s: %v", key, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process image"})
			return
		}

		resizedImg := resize.Resize(uint(width), uint(height), img, resize.Lanczos3)

		var buf bytes.Buffer
		if err := encodeImage(&buf, resizedImg, format); err != nil {
			log.Printf("failed to encode resized image key=%s: %v", key, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process image"})
			return
		}

		c.Header("Content-Type", "image/"+format)
		c.Header("Content-Length", strconv.Itoa(buf.Len()))
		c.Header("Cache-Control", "private, max-age=3600")
		c.Data(http.StatusOK, "image/"+format, buf.Bytes())

	} else {
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

		status := http.StatusOK
		if out.ContentRange != nil {
			c.Header("Content-Range", aws.ToString(out.ContentRange))
			status = http.StatusPartialContent
		}

		c.Status(status)
		if _, err := io.Copy(c.Writer, out.Body); err != nil {
			log.Printf("error streaming key=%s: %v", key, err)
		}
	}
}
