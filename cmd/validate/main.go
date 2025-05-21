// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package validatecmd

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/logg"
	"github.com/spf13/cobra"

	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/models"
)

var (
	authUserName      string
	authPassword      string
	platformFilterStr string
)

// AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "validate <image>...",
		Example: "  keppel validate registry.example.org/library/alpine:3.9",
		Short:   "Pulls an image and validates that its contents are intact.",
		Long: `Pulls an image and validates that its contents are intact.
If the image is in a Keppel replica account, this ensures that the image is replicated as a side effect.`,
		Args: cobra.MinimumNArgs(1),
		Run:  run,
	}
	cmd.PersistentFlags().StringVarP(&authUserName, "username", "u", "", "User name (only required for non-public images).")
	cmd.PersistentFlags().StringVarP(&authPassword, "password", "p", "", "Password (only required for non-public images).")
	cmd.PersistentFlags().StringVar(&platformFilterStr, "platform-filter", "[]", "When validating a multi-architecture image, only recurse into the contained images matching one of the given platforms. The filter must be given as a JSON array of objects matching each having the same format as the `manifests[].platform` field in the <https://github.com/opencontainers/image-spec/blob/master/image-index.md>.")
	parent.AddCommand(cmd)
}

type logger struct{}

// LogManifest implements the client.ValidationLogger interface.
func (l logger) LogManifest(reference models.ManifestReference, level int, err error, isCached bool) {
	indent := strings.Repeat("  ", level)
	suffix := ""
	if isCached {
		suffix = " (cached result)"
	}
	if err == nil {
		logg.Info("%smanifest %s looks good%s", indent, reference, suffix)
	} else {
		logg.Error("%smanifest %s validation failed: %s%s", indent, reference, err.Error(), suffix)
	}
}

// LogBlob implements the client.ValidationLogger interface.
func (l logger) LogBlob(d digest.Digest, level int, err error, isCached bool) {
	indent := strings.Repeat("  ", level)
	suffix := ""
	if isCached {
		suffix = " (cached result)"
	}
	if err == nil {
		logg.Info("%sblob     %s looks good%s", indent, d, suffix)
	} else {
		logg.Error("%sblob     %s validation failed: %s%s", indent, d, err.Error(), suffix)
	}
}

func run(cmd *cobra.Command, args []string) {
	var platformFilter models.PlatformFilter
	err := json.Unmarshal([]byte(platformFilterStr), &platformFilter)
	if err != nil {
		logg.Fatal("cannot parse platform filter: " + err.Error())
	}

	session := client.ValidationSession{
		Logger: logger{},
	}

	for _, arg := range args {
		ref, interpretation, err := models.ParseImageReference(arg)
		logg.Info("interpreting %s as %s", arg, interpretation)
		if err != nil {
			logg.Fatal(err.Error())
		}

		c := &client.RepoClient{
			Host:     ref.Host,
			RepoName: ref.RepoName,
			UserName: authUserName,
			Password: authPassword,
		}
		err = c.ValidateManifest(cmd.Context(), ref.Reference, &session, platformFilter)
		if err != nil {
			os.Exit(1)
		}
	}
}
