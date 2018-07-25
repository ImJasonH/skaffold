/*
Copyright 2018 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package docker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	cstorage "cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/cloud-builders/gcs-fetcher/pkg/uploader"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
	"github.com/pkg/errors"
)

// NormalizeDockerfilePath returns the absolute path to the dockerfile.
func NormalizeDockerfilePath(context, dockerfile string) (string, error) {
	if filepath.IsAbs(dockerfile) {
		return dockerfile, nil
	}

	if !strings.HasPrefix(dockerfile, context) {
		dockerfile = filepath.Join(context, dockerfile)
	}
	return filepath.Abs(dockerfile)
}

func CreateDockerTarContext(buildArgs map[string]*string, w io.Writer, context, dockerfilePath string) error {
	paths, err := GetDependencies(buildArgs, context, dockerfilePath)
	if err != nil {
		return errors.Wrap(err, "getting relative tar paths")
	}
	if err := util.CreateTar(w, context, paths); err != nil {
		return errors.Wrap(err, "creating tar gz")
	}
	return nil
}

func CreateDockerTarGzContext(buildArgs map[string]*string, w io.Writer, context, dockerfilePath string) error {
	paths, err := GetDependencies(buildArgs, context, dockerfilePath)
	if err != nil {
		return errors.Wrap(err, "getting relative tar paths")
	}
	if err := util.CreateTarGz(w, context, paths); err != nil {
		return errors.Wrap(err, "creating tar gz")
	}
	return nil
}

func UploadContextToGCS(ctx context.Context, context, dockerfilePath, bucket, objectName string) error {
	c, err := cstorage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	w := c.Bucket(bucket).Object(objectName).NewWriter(ctx)
	if err := CreateDockerTarGzContext(map[string]*string{}, w, context, dockerfilePath); err != nil {
		return errors.Wrap(err, "uploading targz to google storage")
	}
	return w.Close()
}

func IncrementallyUploadContextToGCS(ctx context.Context, context, dockerfilePath, bucket string) (string, error) {
	c, err := cstorage.NewClient(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close()

	manifestObject := "manifest.json" // TODO: generate unique name.
	up := uploader.New(ctx, realGCS{c}, realOS{}, bucket, manifestObject, 10)

	// Enqueue dependency paths.
	paths, err := GetDependencies(map[string]*string{}, context, dockerfilePath)
	for _, p := range paths {
		slashPath := filepath.ToSlash(p)

		if !filepath.IsAbs(p) {
			p = filepath.Join(context, p)
		}
		info, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		up.Enqueue(slashPath, info)
	}

	// Wait for all workers to finish, or for some error.
	if err := up.Wait(ctx); err != nil {
		return "", err
	}
	return manifestObject, nil
}

// realGCS is a wrapper over the GCS client functions.
type realGCS struct{ client *cstorage.Client }

func (gp realGCS) NewWriter(ctx context.Context, bucket, object string) io.WriteCloser {
	return gp.client.Bucket(bucket).Object(object).
		If(cstorage.Conditions{DoesNotExist: true}). // Skip upload if already exists.
		NewWriter(ctx)
}

// realOS merely wraps the os package implementations.
type realOS struct{}

func (realOS) EvalSymlinks(path string) (string, error) { return filepath.EvalSymlinks(path) }
func (realOS) Stat(path string) (os.FileInfo, error)    { return os.Stat(path) }
