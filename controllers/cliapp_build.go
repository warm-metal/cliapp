package controllers

import (
	"bytes"
	"context"
	"fmt"
	buildkit "github.com/moby/buildkit/client"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	"golang.org/x/xerrors"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"
)

type ImageBuilder struct {
	client *buildkit.Client
	appMap map[string]*imageBuilderContext
}

func InitImageBuilderOrDie(endpoint string) ImageBuilder {
	if len(endpoint) == 0 {
		return ImageBuilder{}
	}

	client, err := buildkit.New(context.TODO(), endpoint, buildkit.WithFailFast())
	if err != nil {
		panic(err)
	}

	timed, cancel := context.WithTimeout(context.TODO(), 3*time.Second)
	_, err = client.ListWorkers(timed)
	cancel()

	return ImageBuilder{
		appMap: make(map[string]*imageBuilderContext),
		client: client,
	}
}

type imageBuilderContext struct {
	ctx        context.Context
	cancel     context.CancelFunc
	client     *buildkit.Client
	Name       string
	Dockerfile string
	Image      string
	Stdout     bytes.Buffer
	Error      error
	Done       bool
}

func (b *imageBuilderContext) finish(err error) {
	b.Error = err
	b.Done = true
}

func (b *imageBuilderContext) fallback() {
	b.cancel()
}

func (b *imageBuilderContext) start() {
	defer b.cancel()
	dockerfile := fmt.Sprintf("%s.dockerfile")
	dockerfileUrl, err := url.Parse(b.Dockerfile)
	if err != nil {
		err = ioutil.WriteFile(dockerfile, []byte(b.Dockerfile), 0644)
		if err != nil {
			b.finish(err)
			return
		}
	}

	if dockerfileUrl.Scheme != "http" && dockerfileUrl.Scheme != "https" {
		err = downloadFile(dockerfile, b.Dockerfile)
		if err != nil {
			b.finish(err)
			return
		}
	}

	solveOpt := buildkit.SolveOpt{
		LocalDirs: map[string]string{
			"context":    ".",
			"dockerfile": ".",
		},
		FrontendAttrs: map[string]string{
			"filename": dockerfile,
		},
		Exports: []buildkit.ExportEntry{
			{
				Type: "image",
				Attrs: map[string]string{
					"name": b.Image,
				},
			},
		},
	}

	if _, err = b.client.Solve(b.ctx, nil, solveOpt, nil); err != nil {
		b.finish(xerrors.Errorf("%s", err))
		return
	}

	b.finish(nil)
}

func downloadFile(filepath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	out, err := os.OpenFile(filepath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

var underBuild = xerrors.Errorf("image is under build")

func (b *ImageBuilder) testImage(app *appcorev1.CliApp) (image string, err error) {
	if ctx, found := b.appMap[app.Name]; found && ctx.Done {
		return ctx.Image, ctx.Error
	}

	remoteCtx, cancel := context.WithCancel(context.TODO())
	ctx := &imageBuilderContext{
		ctx:        remoteCtx,
		cancel:     cancel,
		Name:       app.Name,
		Image:      fmt.Sprintf("docker.io/warmmetal/%s:v1", app.Name),
		Dockerfile: app.Spec.Dockerfile,
		Error:      underBuild,
	}

	b.appMap[app.Name] = ctx
	go ctx.start()
	return ctx.Image, ctx.Error
}

func (b *ImageBuilder) cancel(app *appcorev1.CliApp) {
	if ctx, found := b.appMap[app.Name]; found {
		ctx.fallback()
		delete(b.appMap, app.Name)
		return
	}
}
