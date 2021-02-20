package controllers

import (
	"bytes"
	"context"
	"fmt"
	"github.com/go-logr/logr"
	buildkit "github.com/moby/buildkit/client"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	"golang.org/x/xerrors"
	"io/ioutil"
	"net/url"
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
	log        logr.Logger
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
	solveOpt := buildkit.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: map[string]string{},
		Exports: []buildkit.ExportEntry{
			{
				Type: "image",
				Attrs: map[string]string{
					"name": b.Image,
				},
			},
		},
	}

	dockerfileUrl, err := url.Parse(b.Dockerfile)
	if err != nil {
		dockerfile := fmt.Sprintf("%s.dockerfile", b.Name)
		if err = ioutil.WriteFile(dockerfile, []byte(b.Dockerfile), 0644); err != nil {
			b.finish(err)
			return
		}

		solveOpt.FrontendAttrs["filename"] = dockerfile
		solveOpt.LocalDirs = map[string]string{
			"context":    ".",
			"dockerfile": ".",
		}
	} else if dockerfileUrl.Scheme == "http" || dockerfileUrl.Scheme == "https" {
		solveOpt.FrontendAttrs["context"] = b.Dockerfile
	} else {
		b.finish(xerrors.Errorf("invalid dockerfile"))
		return
	}

	b.log.Info("build image", "opts", solveOpt)
	if _, err = b.client.Solve(b.ctx, nil, solveOpt, nil); err != nil {
		b.finish(xerrors.Errorf("%s", err))
		return
	}

	b.finish(nil)
}

var underBuild = xerrors.Errorf("image is under build")

func (b *ImageBuilder) testImage(log logr.Logger, app *appcorev1.CliApp) (image string, err error) {
	if ctx, found := b.appMap[app.Name]; found && ctx.Done {
		return ctx.Image, ctx.Error
	}

	remoteCtx, cancel := context.WithCancel(context.TODO())
	ctx := &imageBuilderContext{
		log:        log,
		client:     b.client,
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
