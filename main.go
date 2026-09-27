package main

import (
	"database/sql"
	"fmt"

	"github.com/julienschmidt/httprouter"
	_ "github.com/mattn/go-sqlite3"
)

var router *httprouter.Router
var db *sql.DB

type Article struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

func catch(err error) {
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
}

func main() {
	router = httprouter.New()

	var err error
	db, err = connect()
	catch(err)

	router.GET("/", GetAllArticles)

	router.GET("/articles", NewArticle)
	router.POST("/articles", CreateArticle)

	router.GET("/articles/:articleID", ArticleCtx(GetArticle))

	router.PUT("/articles/:articleID", ArticleCtx(UpdateArticle))

	router.DELETE("/articles/:articleID", ArticleCtx(DeleteArticle))

	router.GET("/articles/:articleID/edit", ArticleCtx(EditArticle))

	handler := middleware.Recpv
}
