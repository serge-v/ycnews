package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/doc"
	"html"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	fzf "github.com/junegunn/fzf/src"
	"github.com/pkg/errors"
)

var (
	itemLine     = flag.String("line", "", "article record to open or preview. `RECORD` should be 'whatever [ITEM_ID]'")
	preview      = flag.Bool("preview", false, "preview article")
	listTopics   = flag.Bool("list", false, "list topics")
	urlOpener    = flag.String("open", "", "open article URL in `PROGRAM`")
	viewComments = flag.Bool("view-comments", false, "list comments for the article")
)

var (
	maxItem    = "https://hacker-news.firebaseio.com/v0/maxitem.json"
	topStories = "https://hacker-news.firebaseio.com/v0/topstories.json"
	cacheDir   string

	elinksInstalled  bool
	firefoxInstalled bool
	fzfInstalled     bool
)

const (
	cBlack = 30 + iota
	cRed
	cGreen
	cYellow
	cBlue
	cMagenta
	cCyan
	cWhite
)

func init() {
	cacheDir = os.Getenv("HOME") + "/.cache/ycnews/"
	if err := os.MkdirAll(cacheDir, 0777); err != nil {
		panic(err)
	}

	cmd := exec.Command("which", "fzf")
	if err := cmd.Run(); err == nil {
		fzfInstalled = true
	}
	cmd = exec.Command("which", "elinks")
	if err := cmd.Run(); err == nil {
		elinksInstalled = true
	}
	cmd = exec.Command("which", "firefox")
	if err := cmd.Run(); err == nil {
		firefoxInstalled = true
	}
}

func fetch(u string, cache bool) ([]byte, error) {
	pu, err := url.Parse(u)
	fname := strings.Replace(pu.Path, "/v0/", "", 1)
	fname = strings.Replace(fname, "/", "-", -1)
	fname = cacheDir + fname
	var buf []byte
	if cache {
		buf, err := ioutil.ReadFile(fname)
		//	println("read fname:", fname)
		if err == nil {
			return buf, nil
		}
	}
	resp, err := http.Get(u)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get "+u)
	}
	defer resp.Body.Close()
	buf, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "cannnot read body for "+u)
	}
	if cache {
		ioutil.WriteFile(fname, buf, 0666)
	}
	return buf, nil
}

func unmarshal(url string, d interface{}, cache bool) error {
	buf, err := fetch(url, cache)
	if err != nil {
		return errors.Wrap(err, "cannot fetch "+url)
	}
	err = json.Unmarshal(buf, d)
	if err != nil {
		return errors.Wrap(err, "cannnot unmarshal body for "+url)
	}
	return nil
}

type stories []int

type item struct {
	By          string `json:"by"`
	Descendants int    `json:"descendants"`
	ID          int    `json:"id"`
	Kids        []int  `json:"kids"`
	Score       int    `json:"score"`
	Time        int64  `json:"time"`
	Title       string `json:"title"`
	Text        string `json:"text"`
	Type        string `json:"type"`
	URL         string `json:"url"`
}

func printTopStories() error {
	var st stories
	err := unmarshal(topStories, &st, false)
	if err != nil {
		return errors.Wrap(err, "error fetching topstories")
	}
	cnt := len(st)

	for i := 0; i < cnt; i++ {
		var it item
		itemURL := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", st[i])
		err = unmarshal(itemURL, &it, true)
		if err != nil {
			fmt.Printf("%+v: error fetching\n", st[i])
			continue
		}
		score := fmt.Sprintf("%4d", it.Score)
		if it.Score > 1000 {
			score = fmt.Sprintf("\x1b[31m%4d\x1b[0m", it.Score)
		} else if it.Score > 500 {
			score = fmt.Sprintf("\x1b[33m%4d\x1b[0m", it.Score)
		}
		fmt.Printf("%s  %s  %4d  %s [%d]\n", time.Unix(it.Time, 0).Format("2006-01-02 15:04"), score, len(it.Kids), it.Title, it.ID)
	}

	return nil
}

func isMissing(installed bool, name string) string {
	if installed {
		return ""
	}
	return "\x1b[31m(" + name + "is not installed)\x1b[0m"
}

func printItem(it *item) {
	fmt.Println("\x1b[31mF2\x1b[0m          -- open URL in elinks", isMissing(elinksInstalled, "elinks"))
	fmt.Println("\x1b[31mF3\x1b[0m          -- open URL in firefox", isMissing(firefoxInstalled, "firefox"))
	fmt.Println("\x1b[31mF4 or Enter\x1b[0m -- open comments in less (Q -- quit)")
	fmt.Println("\x1b[31mF10\x1b[0m         -- quit\n\n\n")

	fmt.Println("time: ", time.Unix(it.Time, 0).Format("2006-01-02 15:04:05"))
	fmt.Println("by:   ", it.By)
	fmt.Println("title:", it.Title)
	fmt.Println("score:   ", it.Score)
	fmt.Println("id:      ", it.ID)
	fmt.Println("comments:", len(it.Kids))
	fmt.Println("url:", it.URL)

}

var reHref = regexp.MustCompile("(?s)<a.href=\"([^\"]*)\".*</a>")

func printComments(it *item, indent string) {
	fmt.Printf("\x1b[33m%s\x1b[0m\n\n", it.Title)
	for _, n := range it.Kids {
		itemURL := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", n)
		var cit item
		err := unmarshal(itemURL, &cit, true)
		if err != nil {
			fmt.Printf("%s\n", err.Error())
		} else {
			fmt.Printf("%s\x1b[33m[%s]\x1b[0m\n", indent, cit.By)
			s := html.UnescapeString(cit.Text)
			s = reHref.ReplaceAllString(s, "[\x1b[35m$1\x1b[0m]")
			s = strings.Replace(s, "<p>", "\n\n", -1)
			s = strings.Replace(s, "<i>", "\x1b[1;36m", -1)
			s = strings.Replace(s, "</i>", "\x1b[0m", -1)
			s = strings.Replace(s, "<pre><code> ", "\x1b[32m", -1)
			s = strings.Replace(s, "</code></pre>", "\x1b[0m\n", -1)
			doc.ToText(os.Stdout, s, indent, indent, 72)
			printComments(&cit, indent+"\t")
		}
	}
}

func getItem(itemDescr string) (*item, error) {
	parts := strings.SplitAfter(itemDescr, "[")
	if len(parts) < 2 {
		return nil, errors.New("invalid item token")
	}
	sid := strings.Trim(parts[1], "]")
	id, err := strconv.Atoi(sid)
	if err != nil {
		return nil, errors.Wrap(err, "cannot convert item id")
	}

	itemURL := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id)
	var it item
	err = unmarshal(itemURL, &it, true)
	if err != nil {
		return nil, errors.Wrap(err, "error fetching")
	}
	return &it, nil
}

func openBrowser(opener, u string) {
	cmd := exec.Command(opener, u)
	if opener == "elinks" || opener == "lynx" {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			println(err.Error())
		}
	} else {
		cmd.Start()
	}
}

func main() {
	flag.Parse()
	if *urlOpener != "" {
		it, err := getItem(*itemLine)
		if err != nil {
			println(err.Error())
			return
		}
		openBrowser(*urlOpener, it.URL)
		return
	}
	if *preview {
		it, err := getItem(*itemLine)
		if err != nil {
			println(err.Error())
			return
		}
		printItem(it)
		return
	}
	if *viewComments {
		it, err := getItem(*itemLine)
		if err != nil {
			println(err.Error())
			return
		}
		printComments(it, "")
		return
	}
	if *listTopics {
		if err := printTopStories(); err != nil {
			println(err.Error())
		}
		return
	}

	prog := os.Args[0]
	fzfargs := []string{
		`--ansi`,
		`--preview=` + prog + ` -preview -line {}`,
		`--preview-window=right:40%:wrap`,
		`--bind=alt-p:preview-up,alt-n:preview-down,alt-u:preview-page-up,alt-d:preview-page-down`,
		`--bind=f10:abort`,
		`--bind=f2:execute(` + prog + ` -open elinks -line {})`,
		`--bind=f3:execute(` + prog + ` -open firefox -line {})`,
		`--bind=f4:execute(` + prog + ` -view-comments -line {} | less -r)`,
		`--bind=enter:execute(` + prog + ` -view-comments -line {} | less -r)`,
	}

	if err := os.Setenv("FZF_DEFAULT_COMMAND", prog+" -list"); err != nil {
		panic(err.Error())
	}

	fzf.Run(fzf.ParseArguments(fzfargs), "0.1-dev")

}
