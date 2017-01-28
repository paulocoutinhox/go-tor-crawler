package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/metal3d/go-slugify"
	"golang.org/x/net/proxy"
)

type Site struct {
	URL          string   `json:"url"`
	Title        string   `json:"title"`
	FetchSuccess bool     `json:"fetch_success"`
	Images       []*Image `json:"images"`
}

type Image struct {
	URL          string `json:"url"`
	FetchSuccess bool   `json:"fetch_success"`
}

type ConfigurationFile struct {
	Sites []*Site `json:"sites"`
}

var (
	configuration         *ConfigurationFile
	torDialer             proxy.Dialer
	timeout               time.Duration = (30 * time.Second)
	fileMode              os.FileMode   = 0777
	useAbsolutePath                     = false
	configurationFileName string
)

func main() {
	// read configuration arg
	if len(os.Args) != 2 {
		fmt.Printf("Usage : %s <configuration file> \n", os.Args[0])
		os.Exit(0)
	}

	configurationFileName = os.Args[1]

	// read configuration file content
	currentDir, err := os.Getwd()

	if err != nil {
		fmt.Println("Unable to get current directory:", err)
		os.Exit(0)
	}

	// read configuration file content
	file, e := ioutil.ReadFile(configurationFileName)

	if e != nil {
		fmt.Printf("Error while read configuration file: %v\n", e)
		os.Exit(0)
	}

	// parse configuration file
	err = json.Unmarshal(file, &configuration)

	if err != nil {
		fmt.Println("Unable to parse configuration file:", err)
		os.Exit(0)
	}

	// check sites
	if len(configuration.Sites) == 0 {
		fmt.Println("Site list is empty")
		os.Exit(0)
	}

	// setup localhost TOR proxy
	torProxyURL, err := url.Parse("socks5://127.0.0.1:9050")

	if err != nil {
		fmt.Println("Unable to parse URL:", err)
		os.Exit(0)
	}

	// setup a proxy dialer
	torDialer, err = proxy.FromURL(torProxyURL, proxy.Direct)

	if err != nil {
		fmt.Println("Unable to setup Tor proxy:", err)
		os.Exit(0)
	}

	// get all page contents of site list
	var totalOfSites = len(configuration.Sites)

	for i, site := range configuration.Sites {
		fmt.Println(fmt.Sprintf("Getting site %d of %d - %s...", i+1, totalOfSites, site.URL))

		needDownloadHTML := true

		if site.FetchSuccess {
			needDownloadHTML = false
		}

		// create structure
		var pageContent []byte

		siteDirPreparedName := site.URL
		siteDirPreparedName = strings.Replace(siteDirPreparedName, "http://", "", -1)
		siteDirPreparedName = strings.Replace(siteDirPreparedName, "https://", "", -1)
		siteDirPreparedName = strings.Replace(siteDirPreparedName, ".onion", "", -1)
		siteDirPreparedName = slugify.Marshal(siteDirPreparedName)

		siteDir := currentDir + string(filepath.Separator) + "sites" + string(filepath.Separator) + siteDirPreparedName
		siteFileName := siteDir + string(filepath.Separator) + "index.html"

		if needDownloadHTML {
			torTransport := &http.Transport{Dial: torDialer.Dial}
			client := &http.Client{Transport: torTransport, Timeout: timeout}

			// get page data
			response, err := client.Get(site.URL)

			if err != nil {
				fmt.Println("Unable to fetch site:", site.URL)
				site.FetchSuccess = false
				continue
			}

			defer response.Body.Close()

			// get page body content
			body, err := ioutil.ReadAll(response.Body)

			if err != nil {
				fmt.Println("Unable to get site content:", site.URL)
				site.FetchSuccess = false
				continue
			}

			pageContent = body
		} else {
			// get existing index.html file
			pageContent, err = ioutil.ReadFile(siteFileName)

			if err != nil {
				fmt.Println("Site index.html was not found:", err)
				continue
			}

			fmt.Println("Site already fetched:", site.URL)
		}

		err = os.MkdirAll(siteDir, fileMode)

		if err != nil {
			fmt.Println("Unable to create site directory:", err)
			os.Exit(0)
		}

		// get page title
		htmlTitle := getTagContentFromHTML(string(pageContent), "title", "")
		site.Title = htmlTitle

		// get images
		var images []*Image

		if needDownloadHTML || site.Images == nil {
			images = getAllImagesFromHTML(string(pageContent), site.URL)
		} else {
			images = site.Images
		}

		totalOfImages := len(images)
		downloadedImages := 0

		for imageIndex, image := range images {
			if image.FetchSuccess {
				fmt.Println("Image already fetched:", image.URL)
				downloadedImages++
				continue
			}

			imageURL := site.URL + "/" + image.URL
			imageFileName := siteDir + string(filepath.Separator) + image.URL
			imageFileExists := false

			if useAbsolutePath {
				pageContent = []byte(strings.Replace(string(pageContent), "src=\"", "src=\""+site.URL+"/", -1))
			} else {
				pageContent = []byte(strings.Replace(string(pageContent), "src=\""+site.URL+"/", "src=\"", -1))
			}

			fmt.Println(fmt.Sprintf("Downloading image %d of %d - %s...", imageIndex+1, totalOfImages, imageURL))

			if _, err := os.Stat(imageFileName); err == nil {
				fmt.Println(fmt.Sprintf("Image %d of %d already exists - %s...", imageIndex+1, totalOfImages, imageURL))
				imageFileExists = true
			}

			if imageFileExists {
				image.FetchSuccess = true
				downloadedImages++
			} else {
				err = downloadFile(imageFileName, imageURL)

				if err != nil {
					fmt.Println("Unable to download image:", err)
					continue
				}

				image.FetchSuccess = true
				downloadedImages++
			}
		}

		// reload the images
		site.Images = images

		if downloadedImages == totalOfImages {
			site.FetchSuccess = true
		}

		// prepare and save html content
		err = ioutil.WriteFile(siteFileName, pageContent, fileMode)

		if err != nil {
			fmt.Println("Unable to save site content:", err)
			os.Exit(0)
		}

		saveConfigurationFile()
	}

	saveConfigurationFile()

	fmt.Println("SUCCESS")
}

func getTagContentFromHTML(html string, tagName string, defaultResult string) string {
	buffer := bytes.NewBufferString(html)
	doc, err := goquery.NewDocumentFromReader(buffer)

	if err != nil {
		return defaultResult
	}

	title := doc.Find(tagName).Text()
	return title
}

func getAllImagesFromHTML(html string, url string) []*Image {
	result := []*Image{}
	buffer := bytes.NewBufferString(html)
	doc, err := goquery.NewDocumentFromReader(buffer)

	if err != nil {
		return result
	}

	selection := doc.Find("img")

	for _, node := range selection.Nodes {
		for _, attrib := range node.Attr {
			if strings.EqualFold(attrib.Key, "src") {
				attribVal := attrib.Val

				if attribVal != "" {
					fileExt := filepath.Ext(attribVal)

					if isValidImageExtension(fileExt) {
						attribVal := strings.Replace(attribVal, url+"/", "", -1)

						if attribVal[:1] == "/" {
							attribVal = attribVal[1:len(attribVal)]
						}

						newImage := &Image{
							URL: attribVal,
						}

						result = append(result, newImage)
					}
				}
			}
		}
	}

	return result
}

func downloadFile(fileName string, url string) (err error) {
	// create the file
	os.MkdirAll(filepath.Dir(fileName), fileMode)

	out, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer out.Close()

	torTransport := &http.Transport{Dial: torDialer.Dial}
	client := &http.Client{Transport: torTransport, Timeout: timeout}

	// get the file data
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func saveConfigurationFile() {
	// save the configuration file with the new sites and site data
	configurationJSON, err := json.MarshalIndent(configuration, "", "\t")

	if err != nil {
		fmt.Println("Unable to get configuration data to save:", err)
		os.Exit(0)
	}

	err = ioutil.WriteFile(configurationFileName, configurationJSON, fileMode)

	if err != nil {
		fmt.Println("Unable to save configuration file content:", err)
		os.Exit(0)
	}
}

func isValidImageExtension(extension string) bool {
	extension = strings.Replace(extension, ".", "", -1)

	if strings.EqualFold("jpg", extension) {
		return true
	} else if strings.EqualFold("png", extension) {
		return true
	} else if strings.EqualFold("ico", extension) {
		return true
	} else if strings.EqualFold("gif", extension) {
		return true
	} else if strings.EqualFold("jpeg", extension) {
		return true
	} else if strings.EqualFold("svg", extension) {
		return true
	}

	fmt.Println("Image extension is invalid:", extension)

	return false
}
