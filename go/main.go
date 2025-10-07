package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/disintegration/imaging"
)

const (
	maxBytes              = 10 * 1024 * 1024 // 10MB
	cacheControlImmutable = "public, max-age=31536000, immutable"
)

var (
	s3Client   *s3.Client
	bucketName string
)

func main() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(fmt.Errorf("unable to load SDK config, %v", err))
	}
	s3Client = s3.NewFromConfig(cfg)

	bucketName = os.Getenv("BUCKET_NAME")
	if bucketName == "" {
		panic("BUCKET_NAME environment variable is required")
	}

	lambda.Start(handler)
}

func handler(ctx context.Context, ev events.S3ObjectLambdaEvent) (any, error) {
	b, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		log.Printf("DEBUG: Failed to marshal event: %v", err)
	} else {
		log.Printf("DEBUG: Event = %s", string(b))
	}

	u, err := url.Parse(ev.UserRequest.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	targetWidth, err := parseURLToTargetWidth(u)
	if err != nil {
		return nil, err
	}
	log.Printf("DEBUG: Parsed width = %d", targetWidth)

	s3Key, err := parseURLToS3Key(u)
	if err != nil {
		return nil, err
	}
	log.Printf("DEBUG: Parsed S3 key base = %s", s3Key)

	ob, err := fetchOriginalImgFromS3(ctx, bucketName, s3Key)
	if err != nil {
		return nil, err
	}
	defer ob.Close()

	img, ex, err := decodeImage(ob)
	if err != nil {
		return nil, err
	}
	log.Printf("DEBUG: Decoded image format = %s", ex)

	resizedImg, err := resize(img, ex, targetWidth)
	if err != nil {
		return nil, err
	}

	writeErr := writeGetObjectResponse(ctx, ev, ex, resizedImg)
	if writeErr != nil {
		return nil, writeErr
	}
	return nil, nil
}

func parseURLToTargetWidth(u *url.URL) (int, error) {
	allowedWidths := map[int]bool{240: true, 300: true, 460: true, 700: true, 1040: true}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")

	widthStr := parts[len(parts)-1]
	width, err := strconv.Atoi(widthStr)
	if err != nil || !allowedWidths[width] {
		log.Printf("DEBUG: Width parse error or invalid: %v, allowed widths: %v", width, allowedWidths)
		return 0, errors.New("width must be one of 240,300,460,700,1040")
	}
	return width, nil
}

func parseURLToS3Key(u *url.URL) (string, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return strings.Join(parts[:len(parts)-1], "/"), nil
}

func fetchOriginalImgFromS3(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	res, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	if aws.ToInt64(res.ContentLength) > maxBytes {
		return nil, errors.New("source object too large (>10MB)")
	}

	allowedContentTypes := map[string]bool{"image/jpeg": true, "image/png": true}
	ct := aws.ToString(res.ContentType)
	if !allowedContentTypes[ct] {
		log.Printf("DEBUG: Disallowed content type: %s", ct)
	}

	return res.Body, nil
}

func decodeImage(ob io.Reader) (image.Image, string, error) {
	img, format, err := image.Decode(ob)
	if err != nil {
		return nil, "", fmt.Errorf("decode error (jpeg/png only): %v", err)
	}

	allowedExtensions := map[string]bool{"jpeg": true, "jpg": true, "png": true}
	if !allowedExtensions[format] {
		return nil, "", fmt.Errorf("unsupported image format: %s", format)
	}
	return img, format, nil
}

func resize(src image.Image, format string, targetWidth int) (*bytes.Buffer, error) {
	srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
	if targetWidth <= 0 || srcW <= 0 || srcH <= 0 {
		return nil, fmt.Errorf("invalid image dimensions: srcW=%d, srcH=%d, targetWidth=%d", srcW, srcH, targetWidth)
	}

	if targetWidth == srcW {
		return encodeWithConstraints(src, format)
	}

	targetHeight := calculateTargetHeight(srcW, srcH, targetWidth)
	if targetHeight <= 0 {
		return nil, fmt.Errorf("calculated invalid target height: %d", targetHeight)
	}

	resizedImg, err := resizeLanczos(src, targetWidth, targetHeight)
	if err != nil {
		return nil, fmt.Errorf("resize error: %w", err)
	}

	out, err := encodeWithConstraints(resizedImg, format)
	if err != nil {
		return nil, fmt.Errorf("encode error: %w", err)
	}

	return out, nil
}

func resizeLanczos(src image.Image, width, height int) (image.Image, error) {
	resized := imaging.Resize(src, width, height, imaging.Lanczos)
	return resized, nil
}

func calculateTargetHeight(srcW, srcH, targetW int) int {
	return int(math.Round(float64(srcH) * float64(targetW) / float64(srcW)))
}

func encodeWithConstraints(img image.Image, format string) (*bytes.Buffer, error) {
	switch format {
	case "jpeg", "jpg":
		return encodeJPEGConstrained(img)
	case "png":
		return encodePNGConstrained(img)
	default:
		return nil, errors.New("unsupported format: " + format)
	}
}

func encodeJPEGConstrained(img image.Image) (*bytes.Buffer, error) {
	qualities := []int{95, 90, 85, 80, 75, 70, 65, 60}
	for _, q := range qualities {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
		if buf.Len() <= maxBytes {
			return &buf, nil
		}
	}
	return nil, errors.New("cannot satisfy 10MB limit for jpeg")
}

func encodePNGConstrained(img image.Image) (*bytes.Buffer, error) {
	levels := []png.CompressionLevel{png.DefaultCompression, png.BestCompression}
	for _, lvl := range levels {
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: lvl}
		if err := enc.Encode(&buf, img); err != nil {
			return nil, err
		}
		if buf.Len() <= maxBytes {
			return &buf, nil
		}
	}
	return nil, errors.New("cannot satisfy 10MB limit for png")
}

func writeGetObjectResponse(ctx context.Context, ev events.S3ObjectLambdaEvent, contentType string, body *bytes.Buffer) error {
	input := &s3.WriteGetObjectResponseInput{
		RequestRoute:  aws.String(ev.GetObjectContext.OutputRoute),
		RequestToken:  aws.String(ev.GetObjectContext.OutputToken),
		StatusCode:    aws.Int32(http.StatusOK),
		ContentType:   aws.String(contentType),
		CacheControl:  aws.String(cacheControlImmutable),
		ContentLength: aws.Int64(int64(body.Len())),
		Body:          body,
	}

	_, err := s3Client.WriteGetObjectResponse(ctx, input)
	return err
}
