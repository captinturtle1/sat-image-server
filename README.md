# sat-image-server

A simple and efficient REST API built with Go and the Gin framework to manage satellite mission data. This API interacts with AWS services, using DynamoDB for data storage and S3 for satellite image assets.

## Prerequisites

Before you begin, ensure you have the following installed on your local machine:

- Go (version 1.25.1 or higher recommended)  
- Docker (optional, for containerized deployment)  
- AWS CLI configured with your credentials  

## Getting Started

Follow these steps to get the API up and running on your local machine.

### 1. Clone the Repository

```bash
git clone https://github.com/captinturtle1/sat-image-server
cd sat-image-server
```

### 2. Configure Environment Variables

The API requires several environment variables to connect to AWS services. You can set these in your shell or create a `.env` file and use a library like `godotenv`.

Create a file named `.env` in the root of the project:

```dotenv
# AWS Credentials
AWS_ACCESS_KEY_ID="YOUR_AWS_ACCESS_KEY"
AWS_SECRET_ACCESS_KEY="YOUR_AWS_SECRET_KEY"
AWS_REGION="us-east-1"

# AWS Resource Names
MISSION_TABLE="YourDynamoDBTableName"
SAT_IMAGES_BUCKET="YourS3BucketName"
```

**Note**: For production environments, it is highly recommended to use IAM roles instead of hardcoding credentials.

### 3. Install Dependencies

This command will download and install the necessary Go modules defined in `go.mod`.

```bash
go install
```

### 4. Run the Application

Once the dependencies are installed and your environment is configured, you can start the API:
```bash
go run .
```

The server will start and listen for requests on `http://localhost:8080`.

## Running with Docker

You can also build and run the application as a Docker container for a consistent and isolated environment.

### 1. Build the Docker image:

```bash
docker build -t go-mission-api .
```

### 2. Run the Docker container:

Make sure to pass your environment variables to the container using the `-e` flag or an `--env-file`.

```bash
docker run --rm -p 8080:8080 \
  -e AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  -e AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY \
  -e AWS_REGION=$AWS_REGION \
  -e MISSION_TABLE=$MISSION_TABLE \
  -e SAT_IMAGES_BUCKET=$SAT_IMAGES_BUCKET \
  --name mission-api \
  go-mission-api
```

## API Endpoints

The following endpoints are available:

| Method | Endpoint       | Description                                                   |
| ------ | -------------- | ------------------------------------------------------------- |
| GET    | `/ping`        | A simple health check endpoint. Returns `{"message": "pong"}` |
| GET    | `/missions`    | Retrieves a list of all missions from DynamoDB.               |
| GET    | `/mission/:id` | Retrieves a single mission by its unique ID.                  |
| GET    | `/image/:id`   | Retrieves a satellite image by its unique ID from S3.         |

### Example Response for `GET /mission/:id`

```json
{
  "id": "mission-uuid-1234",
  "name": "Mission Alpha Centauri",
  "status": "In Progress",
  "priority": 1,
  "target_satellite_id": "sat-target-5678",
  "observer_satellite_id": "sat-observer-9101",
  "tca": 1672531200,
  "min_range_km": 5.43,
  "collection_window_start": 1672531000,
  "collection_window_end": 1672531400,
  "collection_type": "IMAGERY",
  "pointing_target": "TARGET",
  "image_ids": [
    "img-uuid-abcd",
    "img-uuid-efgh"
  ]
}
```

## Data Schema

The primary data structure used in this API is the `Mission`.

```go
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
```
