package main

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	// "github.com/gin-contrib/static"
	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
)

func serve(port string) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.HTMLRender = loadTemplates("index.tmpl")
	// router.Use(static.Serve("/static/", static.LocalFile("./static", true)))
	router.GET("/", func(c *gin.Context) {
		c.Redirect(302, "/"+randomAlliterateCombo())
	})
	router.GET("/:page", func(c *gin.Context) {
		page := c.Param("page")
		c.Redirect(302, "/"+page+"/edit")
	})
	router.GET("/:page/*command", handlePageRequest)
	router.POST("/update", handlePageUpdate)
	router.POST("/prime", handlePrime)
	router.POST("/lock", handleLock)
	router.POST("/encrypt", handleEncrypt)
	router.DELETE("/oldlist", handleClearOldListItems)
	router.DELETE("/listitem", deleteListItem)

	router.Run(":" + port)
}

func loadTemplates(list ...string) multitemplate.Render {
	r := multitemplate.New()

	for _, x := range list {
		templateString, err := Asset("templates/" + x)
		if err != nil {
			panic(err)
		}

		tmplMessage, err := template.New(x).Parse(string(templateString))
		if err != nil {
			panic(err)
		}

		r.Add(x, tmplMessage)
	}

	return r
}

func handlePageRequest(c *gin.Context) {
	page := c.Param("page")
	command := c.Param("command")
	if len(command) < 2 {
		command = "/ "
	}
	// Serve static content from memory
	if page == "static" {
		filename := page + command
		data, err := Asset(filename)
		if err != nil {
			c.String(http.StatusInternalServerError, "Could not find data")
		}
		c.Data(http.StatusOK, contentType(filename), data)
		return
	}

	version := c.DefaultQuery("version", "ajksldfjl")
	p := Open(page)

	// Disallow anything but viewing locked/encrypted pages
	if (p.IsEncrypted || p.IsLocked) &&
		(command[0:2] != "/v" && command[0:2] != "/r") {
		c.Redirect(302, "/"+page+"/view")
		return
	}

	// Destroy page if it is opened and primed
	if p.IsPrimedForSelfDestruct && !p.IsLocked && !p.IsEncrypted {
		p.Update("*This page has self-destructed. You can not return to it.*\n\n" + p.Text.GetCurrent())
		p.Erase()
	}
	if command == "/erase" {
		if !p.IsLocked && !p.IsEncrypted {
			p.Erase()
			c.Redirect(302, "/"+page+"/edit")
		} else {
			c.Redirect(302, "/"+page+"/view")
		}
		return
	}
	rawText := p.Text.GetCurrent()
	rawHTML := p.RenderedPage

	// Check to see if an old version is requested
	versionInt, versionErr := strconv.Atoi(version)
	if versionErr == nil && versionInt > 0 {
		versionText, err := p.Text.GetPreviousByTimestamp(int64(versionInt))
		if err == nil {
			rawText = versionText
			rawHTML = GithubMarkdownToHTML(rawText)
		}
	}

	// Get history
	var versionsInt64 []int64
	var versionsChangeSums []int
	var versionsText []string
	if command[0:2] == "/h" {
		versionsInt64, versionsChangeSums = p.Text.GetMajorSnapshotsAndChangeSums(60) // get snapshots 60 seconds apart
		versionsText = make([]string, len(versionsInt64))
		for i, v := range versionsInt64 {
			versionsText[i] = time.Unix(v/1000000000, 0).Format("Mon Jan 2 15:04:05 MST 2006")
		}
		versionsText = reverseSliceString(versionsText)
		versionsInt64 = reverseSliceInt64(versionsInt64)
		versionsChangeSums = reverseSliceInt(versionsChangeSums)
	}

	if command[0:2] == "/r" {
		c.Writer.Header().Set("Content-Type", contentType(p.Name))
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, UPDATE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Max")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Data(200, contentType(p.Name), []byte(rawText))
		return
	}
	log.Debug(command)
	log.Debug("%v", command[0:2] != "/e" &&
		command[0:2] != "/v" &&
		command[0:2] != "/l" &&
		command[0:2] != "/h")

	c.HTML(http.StatusOK, "index.tmpl", gin.H{
		"EditPage":    command[0:2] == "/e", // /edit
		"ViewPage":    command[0:2] == "/v", // /view
		"ListPage":    command[0:2] == "/l", // /list
		"HistoryPage": command[0:2] == "/h", // /history
		"DontKnowPage": command[0:2] != "/e" &&
			command[0:2] != "/v" &&
			command[0:2] != "/l" &&
			command[0:2] != "/h",
		"Page":               page,
		"RenderedPage":       template.HTML([]byte(rawHTML)),
		"RawPage":            rawText,
		"Versions":           versionsInt64,
		"VersionsText":       versionsText,
		"VersionsChangeSums": versionsChangeSums,
		"IsLocked":           p.IsLocked,
		"IsEncrypted":        p.IsEncrypted,
		"ListItems":          renderList(rawText),
		"Route":              "/" + page + command,
		"HasDotInName":       strings.Contains(page, "."),
	})
}

func handlePageUpdate(c *gin.Context) {
	type QueryJSON struct {
		Page    string `json:"page"`
		NewText string `json:"new_text"`
	}
	var json QueryJSON
	if c.BindJSON(&json) != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Wrong JSON"})
		return
	}
	if len(json.NewText) > 100000 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Too much"})
		return
	}
	log.Trace("Update: %v", json)
	p := Open(json.Page)
	var message string
	if p.IsLocked {
		message = "Locked"
	} else if p.IsEncrypted {
		message = "Encrypted"
	} else {
		p.Update(json.NewText)
		p.Save()
		message = "Saved"
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": message})
}

func handlePrime(c *gin.Context) {
	type QueryJSON struct {
		Page string `json:"page"`
	}
	var json QueryJSON
	if c.BindJSON(&json) != nil {
		c.String(http.StatusBadRequest, "Problem binding keys")
		return
	}
	log.Trace("Update: %v", json)
	p := Open(json.Page)
	if p.IsLocked {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Locked"})
		return
	} else if p.IsEncrypted {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Encrypted"})
		return
	}
	p.IsPrimedForSelfDestruct = true
	p.Save()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Primed"})
}

func handleLock(c *gin.Context) {
	type QueryJSON struct {
		Page       string `json:"page"`
		Passphrase string `json:"passphrase"`
	}

	var json QueryJSON
	if c.BindJSON(&json) != nil {
		c.String(http.StatusBadRequest, "Problem binding keys")
		return
	}
	p := Open(json.Page)
	if p.IsEncrypted {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Encrypted"})
		return
	}
	var message string
	if p.IsLocked {
		err2 := CheckPasswordHash(json.Passphrase, p.PassphraseToUnlock)
		if err2 != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "Can't unlock"})
			return
		}
		p.IsLocked = false
		message = "Unlocked"
	} else {
		p.IsLocked = true
		p.PassphraseToUnlock = HashPassword(json.Passphrase)
		message = "Locked"
	}
	p.Save()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": message})
}

func handleEncrypt(c *gin.Context) {
	type QueryJSON struct {
		Page       string `json:"page"`
		Passphrase string `json:"passphrase"`
	}

	var json QueryJSON
	if c.BindJSON(&json) != nil {
		c.String(http.StatusBadRequest, "Problem binding keys")
		return
	}
	p := Open(json.Page)
	if p.IsLocked {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Locked"})
		return
	}
	q := Open(json.Page)
	var message string
	if p.IsEncrypted {
		decrypted, err2 := DecryptString(p.Text.GetCurrent(), json.Passphrase)
		if err2 != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "Wrong password"})
			return
		}
		q.Erase()
		q = Open(json.Page)
		q.Update(decrypted)
		q.IsEncrypted = false
		q.IsLocked = p.IsLocked
		q.IsPrimedForSelfDestruct = p.IsPrimedForSelfDestruct
		message = "Decrypted"
	} else {
		currentText := p.Text.GetCurrent()
		encrypted, _ := EncryptString(currentText, json.Passphrase)
		q.Erase()
		q = Open(json.Page)
		q.Update(encrypted)
		q.IsEncrypted = true
		q.IsLocked = p.IsLocked
		q.IsPrimedForSelfDestruct = p.IsPrimedForSelfDestruct
		message = "Encrypted"
	}
	q.Save()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": message})
}

func deleteListItem(c *gin.Context) {
	lineNum, err := strconv.Atoi(c.DefaultQuery("lineNum", "None"))
	page := c.Query("page") // shortcut for c.Request.URL.Query().Get("lastname")
	if err == nil {
		p := Open(page)

		_, listItems := reorderList(p.Text.GetCurrent())
		newText := p.Text.GetCurrent()
		for i, lineString := range listItems {
			// fmt.Println(i, lineString, lineNum)
			if i+1 == lineNum {
				// fmt.Println("MATCHED")
				if strings.Contains(lineString, "~~") == false {
					// fmt.Println(p.Text, "("+lineString[2:]+"\n"+")", "~~"+lineString[2:]+"~~"+"\n")
					newText = strings.Replace(newText+"\n", lineString[2:]+"\n", "~~"+strings.TrimSpace(lineString[2:])+"~~"+"\n", 1)
				} else {
					newText = strings.Replace(newText+"\n", lineString[2:]+"\n", lineString[4:len(lineString)-2]+"\n", 1)
				}
				p.Update(newText)
				break
			}
		}

		c.JSON(200, gin.H{
			"success": true,
			"message": "Done.",
		})
	} else {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
	}
}

func handleClearOldListItems(c *gin.Context) {
	type QueryJSON struct {
		Page string `json:"page"`
	}

	var json QueryJSON
	if c.BindJSON(&json) != nil {
		c.String(http.StatusBadRequest, "Problem binding keys")
		return
	}
	p := Open(json.Page)
	if p.IsEncrypted {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Encrypted"})
		return
	}
	if p.IsLocked {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Locked"})
		return
	}
	lines := strings.Split(p.Text.GetCurrent(), "\n")
	newLines := make([]string, len(lines))
	newLinesI := 0
	for _, line := range lines {
		if strings.Count(line, "~~") != 2 {
			newLines[newLinesI] = line
			newLinesI++
		}
	}
	p.Update(strings.Join(newLines[0:newLinesI], "\n"))
	p.Save()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Cleared"})
}
