// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package healthmonitorcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/containers/image/v5/manifest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/spf13/cobra"

	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

var longDesc = strings.TrimSpace(`
Monitors the health of a Keppel instance. This sets up a Keppel account with
the given name containing a single image, and pulls the image at regular
intervals. The health check result will be published as a Prometheus metric.

The environment variables must contain credentials for authenticating with the
authentication method used by the target Keppel API.
`)

var listenAddress string

var healthmonitorResultGauge = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "keppel_healthmonitor_result",
		Help: "Result from the keppel healthmonitor check.",
	},
)

// AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "healthmonitor <account>",
		Short: "Monitors the health of a Keppel instance.",
		Long:  longDesc,
		Args:  cobra.ExactArgs(1),
		Run:   run,
	}
	cmd.PersistentFlags().StringVarP(&listenAddress, "listen", "l", ":8080", "Listen address for Prometheus metrics endpoint")
	parent.AddCommand(cmd)
}

type healthMonitorJob struct {
	AuthDriver  client.AuthDriver
	AccountName models.AccountName
	RepoClient  *client.RepoClient

	LastResultLock *sync.RWMutex
	LastResult     *bool // nil during initialization, non-nil indicates result of last healthcheck
}

func run(cmd *cobra.Command, args []string) {
	ctx := httpext.ContextWithSIGINT(cmd.Context(), 1*time.Second)
	keppel.SetTaskName("health-monitor")
	prometheus.MustRegister(healthmonitorResultGauge)

	ad, err := client.NewAuthDriver(ctx)
	if err != nil {
		logg.Fatal("while setting up auth driver: %s", err.Error())
	}

	apiUser, apiPassword := ad.CredentialsForRegistryAPI()
	job := &healthMonitorJob{
		AuthDriver:  ad,
		AccountName: models.AccountName(args[0]),
		RepoClient: &client.RepoClient{
			Scheme:   ad.ServerScheme(),
			Host:     ad.ServerHost(),
			RepoName: args[0] + "/healthcheck",
			UserName: apiUser,
			Password: apiPassword,
		},
		LastResultLock: &sync.RWMutex{},
	}

	// run one-time preparations
	err = job.PrepareKeppelAccount(ctx)
	if err != nil {
		logg.Fatal("while preparing Keppel account: %s", err.Error())
	}
	manifestRef, err := job.UploadImage(ctx)
	if err != nil {
		logg.Fatal("while uploading test image: %s", err.Error())
	}

	// expose metrics endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/healthcheck", job.ReportHealthcheckResult)
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		must.Succeed(httpext.ListenAndServeContext(ctx, listenAddress, mux))
	}()

	// enter long-running check loop
	job.ValidateImage(ctx, manifestRef) // once immediately to initialize the metric
	tick := time.Tick(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			job.ValidateImage(ctx, manifestRef)
		}
	}
}

// Creates the Keppel account for this job if it does not exist yet.
func (j *healthMonitorJob) PrepareKeppelAccount(ctx context.Context) error {
	reqBody := map[string]any{
		"account": map[string]any{
			"auth_tenant_id": j.AuthDriver.CurrentAuthTenantID(),
			// anonymous pull access is needed for `keppel server anycastmonitor`
			"rbac_policies": []map[string]any{{
				"match_repository": "healthcheck",
				"permissions":      []string{"anonymous_pull"},
			}},
		},
	}
	reqBodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "/keppel/v1/accounts/"+string(j.AccountName), bytes.NewReader(reqBodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := j.AuthDriver.SendHTTPRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Uploads a minimal complete image (one config blob, one layer blob and one manifest) for testing.
func (j *healthMonitorJob) UploadImage(ctx context.Context) (models.ManifestReference, error) {
	_, err := j.RepoClient.UploadMonolithicBlob(ctx, []byte(minimalImageConfiguration))
	if err != nil {
		return models.ManifestReference{}, err
	}
	_, err = j.RepoClient.UploadMonolithicBlob(ctx, minimalImageLayer())
	if err != nil {
		return models.ManifestReference{}, err
	}
	digest, err := j.RepoClient.UploadManifest(ctx, []byte(minimalManifest), manifest.DockerV2Schema2MediaType, "latest")
	return models.ManifestReference{Digest: digest}, err
}

// Validates the uploaded image and emits the keppel_healthmonitor_result metric accordingly.
func (j *healthMonitorJob) ValidateImage(ctx context.Context, manifestRef models.ManifestReference) {
	err := j.RepoClient.ValidateManifest(ctx, manifestRef, nil, nil)
	if err == nil {
		j.recordHealthcheckResult(true)
	} else {
		j.recordHealthcheckResult(false)
		imageRef := models.ImageReference{
			Host:      j.RepoClient.Host,
			RepoName:  j.RepoClient.RepoName,
			Reference: manifestRef,
		}
		logg.Error("validation of %s failed: %s", imageRef, err.Error())
	}
}

func (j *healthMonitorJob) recordHealthcheckResult(ok bool) {
	if ok {
		healthmonitorResultGauge.Set(1)
	} else {
		healthmonitorResultGauge.Set(0)
	}
	j.LastResultLock.Lock()
	j.LastResult = &ok
	j.LastResultLock.Unlock()
}

// Provides the GET /healthcheck endpoint.
func (j *healthMonitorJob) ReportHealthcheckResult(w http.ResponseWriter, r *http.Request) {
	j.LastResultLock.RLock()
	lastResult := j.LastResult
	j.LastResultLock.RUnlock()

	switch {
	case lastResult == nil:
		http.Error(w, "still starting up", http.StatusServiceUnavailable)
	case *lastResult:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "healthcheck failed", http.StatusInternalServerError)
	}
}
