package heatmap

import (
	"context"
	"errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/sync/errgroup"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"stash-vr/internal/config"
)

var errImageNotFound = errors.New("image not found")
var errScreenshotImageNotFound = errors.New("screenshot image not found")
var errHeatmapImageNotFound = errors.New("heatmap image not found")

func fetchImage(ctx context.Context, fileUrl string) (image.Image, error) {
	log.Ctx(ctx).Trace().Str("url", fileUrl).Msg("Fetching image")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileUrl, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		log.Ctx(ctx).Debug().Msg("Image not found")
		return nil, errImageNotFound
	}

	img, format, err := image.Decode(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Ctx(ctx).Trace().Str("format", format).Msg("Fetched image")
	return img, nil
}

func buildHeatmapCover(ctx context.Context, coverUrl string, heatmapUrl string) (image.Image, error) {
	var (
		cover      draw.Image
		coverErr   error
		heatmap    image.Image
		heatmapErr error
	)

	g, _ := errgroup.WithContext(ctx)

	g.Go(func() error {
		img, err := fetchImage(log.Ctx(ctx).With().Str("image", "cover").Logger().WithContext(ctx), coverUrl)
		if err != nil {
			coverErr = errors.Join(errScreenshotImageNotFound, err)
			return coverErr
		}
		dest, ok := img.(draw.Image)
		if !ok {
			dest = image.NewRGBA(img.Bounds())
			draw.Copy(dest, image.Pt(0, 0), img, img.Bounds(), draw.Src, nil)
		}
		cover = dest
		return nil
	})

	g.Go(func() error {
		img, err := fetchImage(log.Ctx(ctx).With().Str("image", "heatmap").Logger().WithContext(ctx), heatmapUrl)
		if err != nil {
			heatmapErr = errors.Join(errHeatmapImageNotFound, err)
			return heatmapErr
		}
		heatmap = img
		return nil
	})

	_ = g.Wait()

	if coverErr != nil {
		return nil, coverErr
	}
	if heatmapErr != nil {
		// No heatmap available: serve the plain screenshot instead.
		return cover, nil
	}

	return overlay(cover, heatmap), nil
}

func overlay(dest draw.Image, heatmap image.Image) image.Image {
	destSize := dest.Bounds().Size()
	heatmapHeight := config.Application().HeatmapHeightPx
	if heatmapHeight == 0 {
		heatmapHeight = heatmap.Bounds().Size().Y
	}
	heatmapHeight = int(math.Min(float64(destSize.Y), float64(heatmapHeight)))
	draw.NearestNeighbor.Scale(dest, image.Rect(0, destSize.Y, destSize.X, destSize.Y-heatmapHeight), heatmap, heatmap.Bounds(), draw.Src, nil)
	return dest
}
