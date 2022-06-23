package main

import (
	"fmt"
	"github.com/mmcdole/gofeed"
	"github.com/schollz/progressbar/v3"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	CLEAR  = "\u001b[0m"
	WHITE  = "\u001b[37;5;97m"
	YELLOW = "\u001B[37;5;93m"
	RED    = "\u001b[31m"
	GREEN  = "\u001B[37;5;92m"
)

type PodcastItem struct {
	origin string
	title  string
	date   *time.Time
	link   string
}

var (
	srcFileName = "podcast_sources.txt"
	memFileName = "downloaded_memory.txt"
	dumpPath    = ""
	dryRun      = false
	memory      = map[string]struct{}{}
)

func main() {
	args := os.Args[1:]
	setup(args)

	links := loadLinks(srcFileName)
	memory = loadMemoryFile()

	podcastItems := getItems(links)

	downloaded := 0
	for index, item := range podcastItems {
		downloaded += downloadItem(item, index, len(podcastItems))
	}

	updateMemoryFile(memory)

	if downloaded == 0 {
		fmt.Println(WHITE + "No files downloaded." + CLEAR)
	} else {
		fmt.Println(GREEN + "Successfully downloaded " + strconv.Itoa(downloaded) + " files." + CLEAR)
	}
}

func setup(args []string) {
	if len(args) != 0 {
		// use input arguments
		if len(args) != 3 || len(args) != 4 {
			fmt.Println(RED + "Invalid number of arguments (" + strconv.Itoa(len(args)) + "). Expected 3 or 4 or none." + CLEAR)
			os.Exit(1)
		}
		srcFileName = args[0]
		memFileName = args[1]
		dumpPath = args[2]
		if len(args) == 4 {
			if args[3] == "-d" || args[3] == "--dry" {
				dryRun = true
			} else {
				fmt.Println(YELLOW + "Fourth argument is invalid, expected \"-d\" or \"dry\". I will ignore it." + CLEAR)
			}
		}
	}
}

func loadMemoryFile() map[string]struct{} {
	memRaw, err := os.ReadFile(memFileName)
	if err != nil {
		fmt.Println(RED + "File with previously downloaded links not found." + CLEAR)
		os.Exit(1)
	}
	downloadedLinksMap := make(map[string]struct{})
	for _, line := range strings.Split(string(memRaw), "\n") {
		downloadedLinksMap[line] = struct{}{}
	}
	return downloadedLinksMap
}

func loadLinks(srcFName string) []string {
	srcs, err := os.ReadFile(srcFName)
	if err != nil {
		fmt.Println(RED + "Unable to load file with sources, unexpected error: " + err.Error() + CLEAR)
		os.Exit(1)
	}
	links := strings.Split(strings.TrimSpace(string(srcs)), "\n")
	return links
}

func getItems(links []string) []PodcastItem {
	parser := gofeed.NewParser()
	items := make([]PodcastItem, 0)
	rx := regexp.MustCompile("(<*>*:*\"*/*\\\\*\\|*\\?*\\**)+")

	for _, link := range links {
		raw, err := parser.ParseURL(link)
		if err != nil {
			fmt.Println(YELLOW + "Unable to parse url:" + link + ". Error:" + err.Error() + CLEAR)
			continue
		}
		for _, item := range raw.Items {
			if _, ok := memory[item.Enclosures[0].URL]; ok {
				continue
			}
			p := PodcastItem{
				origin: raw.Title,
				title:  rx.ReplaceAllString(item.Title, ""), // filter out forbidden characters for filenames
				date:   item.PublishedParsed,
				link:   item.Enclosures[0].URL,
			}
			items = append(items, p)
		}
	}

	return items
}

func updateMemoryFile(downloadedLinksMap map[string]struct{}) {
	memS := ""
	for s := range downloadedLinksMap {
		memS += s + "\n"
	}
	err := os.WriteFile(memFileName, []byte(memS), 'w')
	if err != nil {
		fmt.Println(RED + "Unable to update memory file. ERROR: " + err.Error() + CLEAR)
	}
}

func downloadItem(item PodcastItem, index int, total int) int {
	if _, ok := memory[item.link]; ok || dryRun {
		// link is already in memory or dry-run => skip, don't download anything
		return 0
	}

	// get the data
	resp, err := http.Get(item.link)
	if err != nil {
		fmt.Println(YELLOW + "Failed to download file from URL:" + item.link + "ERROR:" + err.Error() + CLEAR)
		return 0
	}
	defer resp.Body.Close()

	// create a new file
	rx := regexp.MustCompile("(<*>*:*\"*/*\\\\*\\|*\\?*\\**)+")
	fileName := item.date.Format("2006-01-02") + " - " + item.origin + " - " + rx.ReplaceAllString(item.title, "") + ".mp3"
	out, err := os.Create(dumpPath + fileName + ".part")
	if err != nil {
		fmt.Println(YELLOW + "Failed to create a new file, ERROR: " + err.Error() + CLEAR)
		return 0
	}
	defer out.Close()

	pbar := progressbar.NewOptions(int(resp.ContentLength),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetDescription("downloading "+strconv.Itoa(index+1)+"/"+strconv.Itoa(total)),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetWidth(20))

	// dump the data into the file
	_, err = io.Copy(io.MultiWriter(out, pbar), resp.Body)
	if err != nil {
		fmt.Println("Failed to copy data to file, ERROR: " + err.Error() + CLEAR)
		return 0
	}

	os.Rename(dumpPath+fileName+".part", dumpPath+fileName) // remove the .part when file is completed
	memory[item.link] = struct{}{}                          // add new item to memory
	pbar.Clear()
	return 1
}