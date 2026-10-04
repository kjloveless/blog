package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"

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

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %v\n%s", err, debug.Stack())
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func ChangeMethod(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTML method overrides use URL-encoded forms; leave multipart uploads
		// unread so UploadHandler can enforce its request size limit first.
		contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Method == http.MethodPost && contentType == "application/x-www-form-urlencoded" {
			switch method := r.PostFormValue("_method"); method {
			case http.MethodPut:
				fallthrough
			case http.MethodPatch:
				fallthrough
			case http.MethodDelete:
				r.Method = method
			default:
			}
		}
		next.ServeHTTP(w, r)
	})
}

type articleContextKey struct{}

func ArticleCtx(next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		id := ps.ByName("articleID")

		article, err := dbGetArticle(id)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "article not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		ctx := context.WithValue(r.Context(), articleContextKey{}, article)

		next(w, r.WithContext(ctx), ps)
	}
}

func GetAllArticles(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	articles, err := dbGetAllArticles()
	catch(err)

	t, err := template.ParseFiles("templates/base.html", "templates/index.html")
	catch(err)
	err = t.Execute(w, articles)
	catch(err)
}

func NewArticle(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	t, err := template.ParseFiles("templates/base.html", "templates/quill.html", "templates/new.html")
	catch(err)
	err = t.Execute(w, nil)
	catch(err)
}

func CreateArticle(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	title := r.FormValue("title")
	content := r.FormValue("content")
	article := &Article{
		Title:   title,
		Content: content,
	}

	err := dbCreateArticle(article)
	catch(err)
	http.Redirect(w, r, "/", http.StatusFound)
}

func GetArticle(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	article := r.Context().Value(articleContextKey{}).(*Article)
	t, err := template.ParseFiles("templates/base.html", "templates/quill.html", "templates/article.html")
	catch(err)
	err = t.Execute(w, article)
	catch(err)
}

func EditArticle(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	article := r.Context().Value(articleContextKey{}).(*Article)

	t, err := template.ParseFiles("templates/base.html", "templates/quill.html", "templates/edit.html")
	catch(err)
	err = t.Execute(w, article)
	catch(err)
}

func UpdateArticle(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	article := r.Context().Value(articleContextKey{}).(*Article)

	title := r.FormValue("title")
	content := r.FormValue("content")
	newArticle := &Article{
		Title:   title,
		Content: content,
	}

	err := dbUpdateArticle(strconv.Itoa(article.ID), newArticle)
	catch(err)
	http.Redirect(w, r, fmt.Sprintf("/articles/%d", article.ID), http.StatusFound)

}

func DeleteArticle(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	article := r.Context().Value(articleContextKey{}).(*Article)
	err := dbDeleteArticle(strconv.Itoa(article.ID))
	catch(err)

	http.Redirect(w, r, "/", http.StatusFound)
}

func UploadHandler(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	const MAX_UPLOAD_SIZE = 10 << 20 // set the max upload size to 10MB
	r.Body = http.MaxBytesReader(w, r.Body, MAX_UPLOAD_SIZE)
	if err := r.ParseMultipartForm(MAX_UPLOAD_SIZE); err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			http.Error(w, "please choose an image smaller than 10 MB", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid image upload", http.StatusBadRequest)
		}
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	defer file.Close()

	// Determine the image type from its contents rather than its filename.
	var sample [512]byte
	n, err := file.Read(sample[:])
	if err != nil && err != io.EOF {
		http.Error(w, "could not read image", http.StatusBadRequest)
		return
	}
	extensions := map[string]string{
		"image/png": ".png", "image/jpeg": ".jpg",
		"image/gif": ".gif", "image/webp": ".webp",
	}
	extension, ok := extensions[http.DetectContentType(sample[:n])]
	if !ok {
		http.Error(w, "please choose a PNG, JPEG, GIF, or WebP image", http.StatusUnsupportedMediaType)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "could not read image", http.StatusInternalServerError)
		return
	}

	// create the uploads folder if it doesn't already exist
	err = os.MkdirAll("./images", os.ModePerm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// create a new file in the uploads directory
	dst, err := os.CreateTemp("./images", "upload-*"+extension)
	if err != nil {
		fmt.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer dst.Close()

	// copy the uploaded file to the specified destination
	_, err = io.Copy(dst, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	location := "/images/" + filepath.Base(dst.Name())
	response, err := json.Marshal(map[string]string{"location": location})
	if err != nil {
		http.Error(w, "could not create image response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(response)
}

func ServeImages(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	fmt.Println(r.URL)
	fs := http.StripPrefix("/images/", http.FileServer(http.Dir("./images")))
	fs.ServeHTTP(w, r)
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

	router.POST("/upload", UploadHandler)
	router.GET("/images/*image", ServeImages)
	router.Handler(http.MethodGet, "/static/*file", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	handler := Recoverer(ChangeMethod(router))

	err = http.ListenAndServe(":8080", handler)
	catch(err)
}
