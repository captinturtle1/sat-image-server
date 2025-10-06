package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
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
	"github.com/disintegration/imaging"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
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

	router.Use(cors.New(cors.Config{
		// AllowOrigins:     []string{"*"}, // For Development
		AllowOrigins:     []string{"https://mission.austinlopez.work"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

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

	limit := int32(10)
	const maxLimit = 100

	countStr := c.Query("count")
	if countStr != "" {
		parsedCount, err := strconv.ParseInt(countStr, 10, 32)
		if err != nil || parsedCount <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'count' parameter. Must be a positive integer."})
			return
		}

		limit = int32(parsedCount)

		if limit > maxLimit {
			limit = maxLimit
		}
	}

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

		var tempKey map[string]map[string]string
		if err := json.Unmarshal(decodedToken, &tempKey); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid pagination token format"})
			return
		}

		exclusiveStartKey := make(map[string]types.AttributeValue)
		for key, valMap := range tempKey {
			for typeIdentifier, value := range valMap {
				switch typeIdentifier {
				case "S":
					exclusiveStartKey[key] = &types.AttributeValueMemberS{Value: value}
				case "N":
					exclusiveStartKey[key] = &types.AttributeValueMemberN{Value: value}
				}
			}
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
		serializableKey := make(map[string]interface{})
		for key, val := range output.LastEvaluatedKey {
			switch v := val.(type) {
			case *types.AttributeValueMemberS:
				serializableKey[key] = map[string]string{"S": v.Value}
			case *types.AttributeValueMemberN:
				serializableKey[key] = map[string]string{"N": v.Value}
			}
		}

		jsonKey, err := json.Marshal(serializableKey)
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
	contrastStr := c.Query("contrast")

	width, _ := strconv.Atoi(widthStr)
	height, _ := strconv.Atoi(heightStr)
	contrast, _ := strconv.ParseFloat(contrastStr, 64)

	needsProcessing := width > 0 || height > 0 || contrast != 0

	in := &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}

	if !needsProcessing {
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

	if needsProcessing {
		srcImage, err := imaging.Decode(out.Body)
		if err != nil {
			log.Printf("failed to decode image key=%s: %v", key, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process image"})
			return
		}

		var processedImage image.Image = srcImage

		if width > 0 || height > 0 {
			processedImage = imaging.Resize(processedImage, width, height, imaging.Lanczos)
		}

		if contrast != 0 {
			processedImage = imaging.AdjustContrast(processedImage, contrast)
		}

		c.Header("Content-Type", "image/jpeg")
		c.Header("Cache-Control", "private, max-age=3600")

		err = imaging.Encode(c.Writer, processedImage, imaging.JPEG, imaging.JPEGQuality(95))
		if err != nil {
			log.Printf("failed to encode and write image key=%s: %v", key, err)
		}

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
