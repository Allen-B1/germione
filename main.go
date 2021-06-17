package main

import (
	"html"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	gemini "github.com/makeworld-the-better-one/go-gemini"
)

func toHTML(body string, originalurl string) template.HTML {
	lines := strings.Split(body, "\n")
	out := ""
	isPre := false
	for _, line := range lines {
		htmlLine := ""

		if isPre {
			if strings.HasPrefix(line, "```") {
				isPre = false
				htmlLine = "</pre>"
			} else {
				htmlLine = html.EscapeString(line)
			}
		} else {
			if strings.HasPrefix(line, "###") {
				htmlLine = "<h3>" + html.EscapeString(line[3:]) + "</h3>"
			} else if strings.HasPrefix(line, "##") {
				htmlLine = "<h2>" + html.EscapeString(line[2:]) + "</h2>"
			} else if strings.HasPrefix(line, "#") {
				htmlLine = "<h1>" + html.EscapeString(line[1:]) + "</h1>"
			} else if strings.HasPrefix(line, "=>") {
				fields := strings.Fields(line)
				url := ""
				text := ""
				if len(fields[0]) > 2 {
					url = fields[0][2:]
					text = strings.Join(fields[1:], " ")
				} else {
					url = fields[1]
					if len(fields) > 2 {
						text = strings.Join(fields[2:], " ")
					}
				}
				if text == "" {
					text = url
				}

				htmlLine = "<p><a href=\"" + geminiToHTTP(url, originalurl) + "\">"
				if strings.HasSuffix(text, ")") && strings.LastIndex(text, "(") != -1 {
					i := strings.LastIndex(text, "(")
					htmlLine += html.EscapeString(text[:i]) + "<span style=\"float:right\">" + html.EscapeString(text[i:]) + "</span></a></p>"
				} else {
					htmlLine += html.EscapeString(text) + "</a></p>"
				}
			} else if strings.HasPrefix(line, "```") {
				htmlLine = "<pre title=\"" + html.EscapeString(strings.TrimSpace(line[3:])) + "\">"
				isPre = true
			} else if strings.HasPrefix(line, "*") {
				htmlLine = "<p class=\"list\">" + html.EscapeString(line[1:]) + "</p>"
			} else if strings.HasPrefix(line, ">") {
				htmlLine = "<p class=\"quote\">" + html.EscapeString(line[1:]) + "</p>"
			} else {
				htmlLine = "<p>" + html.EscapeString(line) + "</p>"
			}
		}

		out += htmlLine + "\n"
	}
	return template.HTML(out)
}

func geminiToHTTP(urlr string, original string) string {
	u, err := url.Parse(urlr)
	if err == nil && u.Scheme != "" {
		if u.Scheme == "gemini" {
			str := "/gemini/" + u.Host + u.Path + "?"
			if u.RawQuery != "" {
				str += "?" + u.RawQuery
			}
			if u.Fragment != "" {
				str += "#" + u.EscapedFragment()
			}
			return str
		} else {
			return u.String()
		}
	}

	if strings.HasPrefix(urlr, "//") {
		return "/gemini/" + urlr[2:]
	} else if strings.HasPrefix(urlr, "/") {
		return "/gemini/" + strings.Split(original, "/")[0] + urlr
	} else {
		return "/gemini/" + path.Join(original, urlr)
	}
}

var client = &gemini.Client{
	ConnectTimeout: 30 * time.Second,
}

var themes = struct {
	m map[string]string
	sync.RWMutex
}{make(map[string]string), sync.RWMutex{}}

// threadsafe
func getTheme(host string) string {
	themes.RLock()
	if theme, ok := themes.m[host]; ok {
		themes.RUnlock()
		return theme
	}
	themes.RUnlock()

	resp, err := client.Fetch("gemini://" + host + "/theme")
	theme := "#000"
	if err == nil && resp.Status == 20 {
		body, err := ioutil.ReadAll(resp.Body)
		if err == nil {
			theme = string(body)
		}
	}

	themes.Lock()
	themes.m[host] = theme
	themes.Unlock()

	return theme
}

func main() {

	r := gin.Default()

	r.SetFuncMap(template.FuncMap{"isnil": func(i interface{}) bool {
		return i == nil
	}, "issearch": func(s string) bool {
		return strings.Contains(strings.ToLower(s), "search")
	}, "splitpath": func(path string) template.HTML {
		parts := strings.Split(path, "?")
		parts2 := strings.Split(parts[0], "/")
		out := template.HTML("")
		for i, part := range parts2 {
			out += "/<a href=\"/gemini/" + template.HTML(html.EscapeString(strings.Join(parts2[:i+1], "/"))) + "\">" + template.HTML(html.EscapeString(part)) + "</a>"
		}
		if len(parts) > 1 {
			out += "?" + template.HTML(strings.Join(parts[1:], "?"))
		}
		return out[1:]
	}})

	r.GET("/search", func(c *gin.Context) {
		path := c.Query("url")
		q := c.Query("q")
		c.Redirect(http.StatusTemporaryRedirect, "/gemini/"+path+"?"+q)
	})

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/gemini/geminispace.info/search")
	})

	r.GET("/gemini/*url", func(c *gin.Context) {
		r.LoadHTMLGlob("templates/*")

		fullpath := c.Param("url")[1:]
		fullpathQuery := fullpath
		if c.Request.URL.RawQuery != "" {
			fullpathQuery += "?" + c.Request.URL.RawQuery
		}

		resp, err := client.Fetch("gemini://" + fullpathQuery)
		parts := strings.Split(fullpath, "/")
		host := parts[0]

		theme := getTheme(host)

		if err != nil {
			c.HTML(http.StatusInternalServerError, "site.tmpl", gin.H{
				"Host":   host,
				"Path":   fullpathQuery,
				"Theme":  theme,
				"Status": "--",
				"Error":  err,
			})
			return
		}

		if resp.Status >= 10 && resp.Status < 20 {
			c.HTML(http.StatusOK, "site.tmpl", gin.H{
				"Host":   host,
				"Path":   fullpathQuery,
				"Theme":  theme,
				"Status": resp.Status,
				"Search": resp.Meta,
			})
		} else if resp.Status >= 20 && resp.Status < 30 {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				c.HTML(http.StatusInternalServerError, "site.tmpl", gin.H{
					"Host":   host,
					"Path":   fullpathQuery,
					"Theme":  theme,
					"Status": "--",
					"Error":  err,
				})
				return
			}
			if strings.Split(resp.Meta, ";")[0] == "text/gemini" {
				c.HTML(http.StatusOK, "site.tmpl", gin.H{
					"Host":    host,
					"Path":    fullpathQuery,
					"Theme":   theme,
					"Status":  resp.Status,
					"Type":    "text/gemini",
					"Content": toHTML(string(body), fullpath),
				})
			} else if strings.Split(resp.Meta, ";")[0] == "text/plain" {
				c.HTML(http.StatusOK, "site.tmpl", gin.H{
					"Host":    host,
					"Path":    fullpathQuery,
					"Theme":   theme,
					"Status":  resp.Status,
					"Type":    "text/plain",
					"Content": string(body),
				})
			} else {
				c.Header("Content-Type", resp.Meta)
				c.Writer.Write(body)
			}
		} else if resp.Status >= 30 && resp.Status < 40 {
			status := 307
			if resp.Status == 31 {
				status = 308
			}

			c.Redirect(status, geminiToHTTP(resp.Meta, fullpath))
		} else {
			c.HTML(http.StatusOK, "site.tmpl", gin.H{
				"Host":   host,
				"Path":   fullpathQuery,
				"Theme":  theme,
				"Status": resp.Status,
				"Error":  resp.Meta,
			})
		}
	})

	r.Run()
}
