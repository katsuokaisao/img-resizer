package main

import (
	"bytes"
	"context"
	"errors"
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
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"golang.org/x/image/draw"
)

const maxBytes = 10 * 1024 * 1024 // 10MB

var (
	s3Client   *s3.Client
	bucketName string
	// 許可する幅
	allowedWidths = map[int]bool{240: true, 300: true, 460: true, 700: true, 1040: true}
)

func main() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	s3Client = s3.NewFromConfig(cfg)

	// バケット名を環境変数から取得
	bucketName = os.Getenv("BUCKET_NAME")
	if bucketName == "" {
		panic("BUCKET_NAME environment variable is required")
	}

	lambda.Start(handler)
}

func handler(ctx context.Context, ev events.S3ObjectLambdaEvent) (any, error) {
	log.Printf("[DEBUG] Event: %+v", ev)
	log.Printf("[DEBUG] GetObjectContext: %+v", ev.GetObjectContext)
	log.Printf("[DEBUG] UserRequest Headers: %+v", ev.UserRequest.Headers)

	// 1) クエリパラメータから幅と高さを取得
	reqURL := ev.UserRequest.URL
	log.Printf("DEBUG: Request URL = %s", reqURL)

	u, err := url.Parse(reqURL)
	if err != nil {
		return nil, writeError(ctx, ev, http.StatusBadRequest, "invalid request URL")
	}

	log.Printf("DEBUG: Path = %s, RawQuery = %s", u.Path, u.RawQuery)

	// クエリパラメータから取得
	query := u.Query()
	log.Printf("DEBUG: Query params: w=%s, h=%s", query.Get("w"), query.Get("h"))

	width, err := strconv.Atoi(query.Get("w"))
	if err != nil || !allowedWidths[width] {
		log.Printf("DEBUG: Width parse error or invalid: %v, allowed widths: %v", width, allowedWidths)
		return nil, writeError(ctx, ev, http.StatusBadRequest, "width must be one of 240,300,460,700,1040")
	}

	// 高さパラメータの取得（指定がない場合は幅と同じ＝正方形）
	height, _ := strconv.Atoi(query.Get("h"))
	if height == 0 {
		height = width
	}

	// 2) パスから元画像のキーを取得
	srcKeyBase := strings.TrimPrefix(u.Path, "/")
	if srcKeyBase == "" {
		return nil, writeError(ctx, ev, http.StatusBadRequest, "empty path")
	}

	// 3) 元画像を S3 GetObject で取得（最大10MBに制限）
	orig, _, err := fetchOriginalFromS3(ctx, bucketName, srcKeyBase)
	if err != nil {
		var nsk *s3types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, writeError(ctx, ev, http.StatusNotFound, "base image not found")
		}
		return nil, writeError(ctx, ev, http.StatusBadGateway, "failed to fetch original: "+err.Error())
	}

	img, format, err := image.Decode(bytes.NewReader(orig))
	if err != nil {
		return nil, writeError(ctx, ev, http.StatusUnsupportedMediaType, "decode error (jpeg/png only): "+err.Error())
	}
	format = strings.ToLower(format) // "jpeg" or "png"

	// 4) 目標サイズの計算。拡大はしない
	srcW, srcH := img.Bounds().Dx(), img.Bounds().Dy()
	targetW := width
	targetH := height

	// 幅が元画像より大きい場合は元画像のサイズに制限
	if targetW > srcW {
		targetW = srcW
		// 高さもアスペクト比を維持して調整
		if height > 0 {
			scale := float64(targetW) / float64(width)
			targetH = int(math.Round(float64(height) * scale))
		} else {
			targetH = srcH
		}
	}

	// 高さが元画像より大きい場合も同様に制限
	if targetH > srcH {
		targetH = srcH
		// 幅もアスペクト比を維持して調整
		scale := float64(targetH) / float64(height)
		targetW = int(math.Round(float64(targetW) * scale))
	}

	if targetW <= 0 || targetH <= 0 {
		return nil, writeError(ctx, ev, http.StatusBadRequest, "calculated size invalid")
	}

	// 5) リサイズ（CatmullRom）
	dst := resize(img, targetW, targetH)

	// 6) フォーマット維持 + 10MB 制約
	out, outCT, err := encodeWithConstraints(dst, format)
	if err != nil {
		return nil, writeError(ctx, ev, http.StatusInternalServerError, "encode error: "+err.Error())
	}

	// 7) 応答（キャッシュ系ヘッダは任意）
	// WriteGetObjectResponseはHTTP APIを直接呼び出す
	writeErr := writeGetObjectResponse(ctx, ev.GetObjectContext.OutputRoute, ev.GetObjectContext.OutputToken, http.StatusOK, outCT, out)
	if writeErr != nil {
		return nil, writeErr
	}
	return nil, nil
}

func writeError(ctx context.Context, ev events.S3ObjectLambdaEvent, code int, msg string) error {
	log.Printf("ERROR %d: %s", code, msg)
	_ = writeGetObjectResponse(ctx, ev.GetObjectContext.OutputRoute, ev.GetObjectContext.OutputToken, code, "text/plain", []byte(msg))
	return errors.New(msg)
}

// writeGetObjectResponse は S3 Object Lambda の WriteGetObjectResponse API を HTTP で直接呼び出す
func writeGetObjectResponse(ctx context.Context, route, token string, statusCode int, contentType string, body []byte) error {
	url := route + "?write=true"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("x-amz-request-token", token)
	req.Header.Set("x-amz-status-code", strconv.Itoa(statusCode))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Cache-Control", "public, max-age=31536000, immutable")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return errors.New("WriteGetObjectResponse failed with status: " + resp.Status)
	}
	return nil
}

// ----- S3 読み取り（baseKeyで） -----

func fetchOriginalFromS3(ctx context.Context, bucket, key string) ([]byte, string, error) {
	// 10MB超はエラー
	getOut, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", err
	}
	defer getOut.Body.Close()

	// Content-Length を先に見て 10MB 超なら早期エラー
	if getOut.ContentLength != nil && *getOut.ContentLength > maxBytes {
		return nil, "", errors.New("source object too large (>10MB)")
	}

	// 念のため読み取りも制限
	lim := io.LimitedReader{R: getOut.Body, N: maxBytes + 1}
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, &lim); err != nil {
		return nil, "", err
	}
	if int64(buf.Len()) > maxBytes {
		return nil, "", errors.New("source object too large (>10MB)")
	}
	ct := aws.ToString(getOut.ContentType)
	return buf.Bytes(), ct, nil
}

// ----- 画像処理ユーティリティ -----

func resize(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func encodeWithConstraints(img image.Image, format string) ([]byte, string, error) {
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		return encodeJPEGConstrained(img)
	case "png":
		return encodePNGConstrained(img)
	default:
		return nil, "", errors.New("unsupported format: " + format)
	}
}

func encodeJPEGConstrained(img image.Image) ([]byte, string, error) {
	qualities := []int{95, 90, 85, 80, 75, 70, 65, 60}
	for {
		for _, q := range qualities {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
				return nil, "", err
			}
			if buf.Len() <= maxBytes {
				return buf.Bytes(), "image/jpeg", nil
			}
		}
		// まだ大きい → 0.9倍
		b := img.Bounds()
		nw := int(float64(b.Dx()) * 0.9)
		nh := int(float64(b.Dy()) * 0.9)
		if nw < 32 || nh < 32 {
			return nil, "", errors.New("cannot satisfy 10MB limit for jpeg")
		}
		img = resize(img, nw, nh)
	}
}

func encodePNGConstrained(img image.Image) ([]byte, string, error) {
	levels := []png.CompressionLevel{png.DefaultCompression, png.BestCompression}
	for {
		for _, lvl := range levels {
			var buf bytes.Buffer
			enc := png.Encoder{CompressionLevel: lvl}
			if err := enc.Encode(&buf, img); err != nil {
				return nil, "", err
			}
			if buf.Len() <= maxBytes {
				return buf.Bytes(), "image/png", nil
			}
		}
		// まだ大きい → 0.9倍
		b := img.Bounds()
		nw := int(float64(b.Dx()) * 0.9)
		nh := int(float64(b.Dy()) * 0.9)
		if nw < 32 || nh < 32 {
			return nil, "", errors.New("cannot satisfy 10MB limit for png")
		}
		img = resize(img, nw, nh)
	}
}
