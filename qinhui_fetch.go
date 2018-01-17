package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/axgle/mahonia"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	tokens = make(chan struct{}, 10) // 多任务令牌
	wg     sync.WaitGroup
)

const (
	aLUNWEN   = 1 // 论文
	aSHIPING  = 2 // 时评
	aSUIBI    = 3 // 随笔
	aZHUZUO   = 4 // 著作
	aYANJIANG = 5 // 演讲
	aDUSHU    = 6 // 读书
	aFANGTAN  = 7 // 访谈
)

var catalog map[int]string = map[int]string{
	aLUNWEN:   "论文",
	aSHIPING:  "时评",
	aSUIBI:    "随笔",
	aZHUZUO:   "著作",
	aYANJIANG: "演讲",
	aDUSHU:    "读书",
	aFANGTAN:  "访谈",
}

type articleItem struct {
	aType        int    // 文章分类
	aTitle, aUrl string // 文章标题和链接
}

type articleContent struct {
	Content string `json:"content"`
}

// 恢复字符串中的转义字符
func convertEscape(in []byte) string {

	// 创建一个零长度的rune
	var chineseStr = make([]rune, 0)
	for i := 0; i < len(in); {
		// 找到要转义的字符
		if in[i] == '\\' && i < len(in)-1 && in[i+1] == 'u' {
			// 把这个取出来 \uXXXX\u...
			runeChar, err := strconv.ParseUint(string(in[i+2:i+6]), 16, 16)
			if err != nil {
				log.Panic(err)
			}
			chineseStr = append(chineseStr, rune(runeChar))
			// 跳过当前字节
			i += 6
		} else {
			chineseStr = append(chineseStr, rune(in[i]))
			i++
		}

	}
	return string(chineseStr)
}

// 获得文章内容
func getArticleContent(artItem *articleItem) {
	// 获得令牌
	tokens <- struct{}{}
	defer func() {
		<-tokens
		wg.Done()
	}()
Retry:
	fmt.Printf("正在抓取：%s,%s\n", artItem.aTitle, artItem.aUrl)
	resp, err := http.Get(artItem.aUrl)
	if err != nil {
		// handle error
		log.Fatal(err)
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		goto Retry
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		log.Fatal(err)
	}

	artContent := articleContent{""}

	fmt.Printf("正在对《%s》内容进行转码并解析...", artItem.aTitle)

	err = json.Unmarshal([]byte(convertEscape(body)), &artContent)
	if err != nil {
		log.Fatal(err)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(artContent.Content)))
	if err != nil {
		// handler error
		log.Fatal(err)
	}

	var artContentSlice []byte = make([]byte, 0)
	// 找到每个段落
	doc.Find("p").Each(func(i int, s *goquery.Selection) {
		artContentSlice = append(artContentSlice, []byte("    "+strings.TrimSpace(s.Text())+"\r\n")...)
	})

	filePath := fmt.Sprintf("./%s", catalog[artItem.aType])
	// 创建目录
	if err = os.MkdirAll(filePath, 0777); err != nil {
		log.Fatal()
	}
	err = ioutil.WriteFile(filePath+"/"+artItem.aTitle+ ".txt", artContentSlice, 0666)

	if err != nil {
		// handler error
		log.Fatal(err)
	} else {
		fmt.Printf("生成文件：%s\n", filePath+"/"+artItem.aTitle)
	}

}

// 获得文章列表
func getArticleList() (lists [][]*articleItem) {

	var artListChan chan []*articleItem = make(chan []*articleItem) // 文章列表channel
	lists = make([][]*articleItem, 0, 8)                            // 文章列表

	resp, err := http.Get("http://www.aisixiang.com/thinktank/qinhui.html")
	if err != nil {
		// handle error
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		log.Fatal(err)
	}

	length, utf8Data, err := mahonia.NewDecoder("gbk").Translate(body, true)
	fmt.Printf("抓取首页..\n转换%d字节从GBK到UTF-8\n", length)
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(utf8Data))
	if err != nil {
		// handler error
		log.Fatal(err)
	}
	fmt.Printf("获取文章列表..\n")
	// 找到每个栏目
	doc.Find("td:has(img+a)").Each(func(i int, s *goquery.Selection) {
		if s.HasClass("bg-no-b") { // 跳过第一个元素
			return
		}
		wg.Add(1)
		go phraseArticle(i, s, artListChan) // 解析文章列表
	})

	go func() {
		wg.Wait()
		close(artListChan)
	}()

	for x := range artListChan {
		lists = append(lists, x)
		fmt.Printf("找到%d篇文章\n", len(x))
	}
	return
}

func getArticleID(link string) string {
	return strings.TrimPrefix(strings.TrimSuffix(link, ".html"), "/data/")
}

// 提取栏目中所有的文章链接和文章，返回slice
func phraseArticle(catalog int, s *goquery.Selection, out chan<- []*articleItem) {
	articleList := make([]*articleItem, 0, 16)
	// 解析每个栏目下面的每个文章标题和链接
	s.Find("img+a").Each(func(i int, s *goquery.Selection) {
		link, ok := s.Attr("href")
		if !ok {
			return
		}
		ptrItem := new(articleItem)
		ptrItem.aTitle = s.Text()
		ptrItem.aType = catalog
		ptrItem.aUrl = "http://www.aisixiang.com/data/view_json.php?id=" + getArticleID(link)
		articleList = append(articleList, ptrItem)
	})
	out <- articleList
	wg.Done()
}

func main() {
	lists := getArticleList()
	// 遍历输出查看结果
	for _, value := range lists {
		for _, value2 := range value {
			wg.Add(1)
			go getArticleContent(value2)
		}
	}

	wg.Wait()
	close(tokens)
	fmt.Println("一切结束！")
	return
}
