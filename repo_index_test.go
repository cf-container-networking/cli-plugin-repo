package main_test

import (
	"encoding/hex"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"code.cloudfoundry.org/cli-plugin-repo/sort/yamlsorter"
	"code.cloudfoundry.org/cli-plugin-repo/web"

	"net/url"

	"crypto/sha1"
	"fmt"
	"net/http"

	"github.com/blang/semver"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("Database", func() {
	It("correctly parses the current repo-index.yml", func() {
		var plugins web.PluginsJson

		b, err := ioutil.ReadFile("repo-index.yml")
		Expect(err).NotTo(HaveOccurred())

		err = yaml.Unmarshal(b, &plugins)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("validations", func() {
		var plugins web.PluginsJson
		var pluginBytes []byte

		BeforeEach(func() {
			var err error
			pluginBytes, err = ioutil.ReadFile("repo-index.yml")
			Expect(err).NotTo(HaveOccurred())

			err = yaml.Unmarshal(pluginBytes, &plugins)
			Expect(err).NotTo(HaveOccurred())
		})

		It("the yaml file is sorted", func() {
			var yamlSorter yamlsorter.YAMLSorter

			sortedBytes, err := yamlSorter.Sort(pluginBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(sortedBytes).To(Equal(pluginBytes), "file is not sorted; please run 'go run sort/main.go repo-index.yml'.\n")
		})

		It("has every binary link over https", func() {
			for _, plugin := range plugins.Plugins {
				for _, binary := range plugin.Binaries {
					url, err := url.Parse(binary.Url)
					Expect(err).NotTo(HaveOccurred())

					Expect(url.Scheme).To(Equal("https"))
				}
			}
		})

		It("has every version parseable by semver", func() {
			for _, plugin := range plugins.Plugins {
				version, err := semver.Make(plugin.Version)
				Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Plugin '%s' has a non-semver version", plugin.Name))
				Expect(version.Validate()).ToNot(HaveOccurred(), fmt.Sprintf("Plugin '%s' has a non-semver version", plugin.Name))
			}
		})

		It("validates the platforms for every binary", func() {
			for _, plugin := range plugins.Plugins {
				for _, binary := range plugin.Binaries {
					Expect(web.ValidPlatforms).To(
						ContainElement(binary.Platform),
						fmt.Sprintf(
							"Plugin '%s' contains a platform '%s' that is invalid. Please use one of the following: '%s'",
							plugin.Name,
							binary.Platform,
							strings.Join(web.ValidPlatforms, ", "),
						))
				}
			}
		})

		It("requires HTTPS for all downloads", func() {
			for _, plugin := range plugins.Plugins {
				for _, binary := range plugin.Binaries {
					Expect(binary.Url).To(
						MatchRegexp("^https|ftps"),
						fmt.Sprintf(
							"Plugin '%s' links to a Binary's URL '%s' that cannot be downloaded over SSL (begins with https/ftps). Please provide a secure download link to your binaries. If you are unsure how to provide one, try out GitHub Releases: https://help.github.com/articles/creating-releases",
							plugin.Name,
							binary.Url,
						))
				}
			}
		})

		It("every binary download had a matching sha1", func() {
			if os.Getenv("BINARY_VALIDATION") != "true" {
				Skip("Skipping SHA1 binary checking. To enable, set the BINARY_VALIDATION env variable to 'true'")
			}

			fmt.Println("\nRunning Binary Validations, this could take 10+ minutes")

			for _, plugin := range plugins.Plugins {
				for _, binary := range plugin.Binaries {
					fmt.Printf("%s checking %s %s\n", time.Now().Format(time.RFC3339), plugin.Name, binary.Platform)
					var err error
					resp, err := http.Get(binary.Url)
					Expect(err).NotTo(HaveOccurred())

					// If there's a network error, retry exactly once for this plugin binary.
					switch resp.StatusCode {
					case http.StatusInternalServerError,
						http.StatusBadGateway,
						http.StatusServiceUnavailable,
						http.StatusGatewayTimeout:
						Expect(resp.Body.Close()).To(Succeed())
						resp, err = http.Get(binary.Url)
						Expect(err).NotTo(HaveOccurred())
					}

					defer resp.Body.Close()
					b, err := ioutil.ReadAll(resp.Body)
					Expect(err).NotTo(HaveOccurred())

					Expect(resp.StatusCode).To(And(BeNumerically(">=", 200), BeNumerically("<", 400)),
						"Failed to retrieve '%s', can't compute SHA from URL %s", plugin.Name, binary.Url)

					s := sha1.Sum(b)
					Expect(hex.EncodeToString(s[:])).To(Equal(binary.Checksum),
						fmt.Sprintf("Plugin '%s' has an invalid checksum for platform '%s'", plugin.Name, binary.Platform))
				}
			}
		})
	})
})
