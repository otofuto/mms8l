package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"mms8l/pkg/database"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/acme/autocert"
)

func main() {
	godotenv.Load()
	port := os.Getenv("PORT")
	if len(os.Args) > 1 {
		port = os.Args[1]
		if port == "ssl" {
			port = "443"
		}
	}
	if port == "" {
		port = "5008"
	}

	mux := http.NewServeMux()
	mux.Handle("/st/", http.StripPrefix("/st/", http.FileServer(http.Dir("./static"))))
	mux.HandleFunc("/", IndexHandle)
	mux.HandleFunc("/log/", LogHandle)
	mux.HandleFunc("/git", GitHandle)
	mux.HandleFunc("/api/", ApiHandle)
	mux.HandleFunc("/img/", ImgHandle)
	mux.HandleFunc("/favicon.ico", FaviconHandle)
	log.Println("Listening on port: " + port)
	if port == "443" {
		log.Println("SSL")
		if err := http.Serve(autocert.NewListener("mms.klcorp.tokyo"), mux); err != nil {
			panic(err)
		}
	} else if err := http.ListenAndServe(":"+port, mux); err != nil {
		panic(err)
	}
}

func IndexHandle(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html; charset=utf-8")

	//UA無しは通さない
	if r.UserAgent() == "" {
		http.Error(w, "Access Denied.", 403)
		return
	} else if strings.HasPrefix(r.UserAgent(), "curl/") {
		//curl禁止
		http.Error(w, "Access Denied.", 403)
		return
	} else if strings.HasPrefix(r.UserAgent(), "python-requests/") {
		//許さない
		http.Error(w, "Access Denied.", 403)
		return
	} else if strings.Index(r.UserAgent(), "AhrefsBot") > 0 {
		http.Error(w, "Access Denied.", 403)
	}

	if r.Method == http.MethodGet {
		context := struct {
			Page          int
			PageLength    int
			Sort          string
			Customer      Customer
			ReturnPath    string
			CustomerGroup CustomerGroup
			Logs          []LinkAccessLog
			LogCount      int
		}{}
		filename := ""
		if r.URL.Path == "/" {
			filename = "index"
			if CheckLogin(r) {
				http.Redirect(w, r, "/top", 303)
				return
			}
		} else if r.URL.Path == "/top" {
			filename = "top"
		} else if r.URL.Path == "/logout" {
			cookie, err := r.Cookie("mmslogin")
			if err != nil {
				http.Redirect(w, r, "/", 303)
				return
			}
			db := database.Connect()
			defer db.Close()
			rows, err := db.Query("delete from admin_login where token = '" + database.Escape(cookie.Value) + "'")
			if err != nil {
				log.Println(err)
				http.Redirect(w, r, "/", 303)
				return
			}
			defer rows.Close()
			cookie.MaxAge = 0
			http.SetCookie(w, cookie)
			http.Redirect(w, r, "/", 303)
			return
		} else if r.URL.Path == "/customer" {
			filename = "customer"
			if r.FormValue("p") != "" {
				context.Page, _ = strconv.Atoi(r.FormValue("p"))
			}
			if r.FormValue("s") != "" {
				context.Sort = r.FormValue("s")
			}
		} else if r.URL.Path == "/customer/edit" {
			filename = "customer_edit"
			if r.FormValue("returnpath") == "" || r.FormValue("id") == "" {
				http.Error(w, "不正なリクエストです", 400)
				return
			}
			context.ReturnPath = r.FormValue("returnpath")
			id, err := strconv.Atoi(r.FormValue("id"))
			if err != nil {
				log.Println(err)
				http.Error(w, "idが数値ではありません", 400)
				return
			}
			db := database.Connect()
			defer db.Close()
			rows, err := db.Query(`select id, manager, email, corp, tel, memo, created_at from customer where id = ` + strconv.Itoa(id))
			if err != nil {
				log.Println(err)
				http.Error(w, "データベースエラー", 500)
				return
			}
			defer rows.Close()
			if rows.Next() {
				err = rows.Scan(&context.Customer.Id, &context.Customer.Manager, &context.Customer.Email, &context.Customer.Corp, &context.Customer.Tel, &context.Customer.Memo, &context.Customer.CreatedAt)
				if err != nil {
					log.Println(err)
					http.Error(w, "データベースエラー", 500)
					return
				}
			} else {
				http.Error(w, "顧客データが見つかりませんでした。", 404)
				return
			}
		} else if r.URL.Path == "/group" {
			filename = "group"
		} else if r.URL.Path == "/group/edit" {
			filename = "group_edit"
			if r.FormValue("id") == "" {
				http.Error(w, "不正なリクエストです", 400)
				return
			}
			id, err := strconv.Atoi(r.FormValue("id"))
			if err != nil {
				log.Println(err)
				http.Error(w, "idが数値ではありません", 400)
				return
			}
			db := database.Connect()
			defer db.Close()
			rows, err := db.Query(`select group_id, customer_id, gname, manager, email from customer_group_member left outer join customer on customer_id = customer.id left outer join customer_group on group_id = customer_group.id where group_id = ` + strconv.Itoa(id))
			if err != nil {
				log.Println(err)
				http.Error(w, "データベースエラー", 500)
				return
			}
			defer rows.Close()
			for rows.Next() {
				var m CustomerGroupMember
				err = rows.Scan(&context.CustomerGroup.Id, &m.Customer.Id, &context.CustomerGroup.GroupName, &m.Customer.Manager, &m.Customer.Email)
				if err != nil {
					log.Println(err)
					http.Error(w, "データベースエラー", 500)
					return
				}
				context.CustomerGroup.Members = append(context.CustomerGroup.Members, m)
			}
			if context.CustomerGroup.GroupName == "" {
				http.Error(w, "データが見つかりませんでした", 404)
				return
			}
		} else if r.URL.Path == "/mail" {
			filename = "mail"
		} else if r.URL.Path == "/history" {
			filename = "history"
		} else if r.URL.Path == "/link" {
			filename = "link"
			page, _ := strconv.Atoi(r.FormValue("p"))
			context.Page = page
			page--
			db := database.Connect()
			defer db.Close()
			maxpageview := 50
			where := ""
			emailid, _ := strconv.Atoi(r.FormValue("emailid"))
			if emailid > 0 {
				where = " where email_id = " + strconv.Itoa(emailid)
				context.ReturnPath = strconv.Itoa(emailid)
			}
			rows, err := db.Query("select email_id, link_number, customer_id, ua, ip, accessed_at, left(title, 15) as title, manager from link_access_log left outer join email on link_access_log.email_id = email.id left outer join customer on link_access_log.customer_id = customer.id" + where + " order by accessed_at desc limit " + strconv.Itoa(maxpageview) + " offset " + strconv.Itoa(page*maxpageview))
			if err != nil {
				log.Println(err)
				http.Error(w, "データベースエラー", 500)
				return
			}
			defer rows.Close()
			for rows.Next() {
				var lal LinkAccessLog
				err = rows.Scan(&lal.EmailId, &lal.LinkNumber, &lal.CustomerId, &lal.UA, &lal.IP, &lal.AccessedAt, &lal.EmailTitle, &lal.Manager)
				if err != nil {
					log.Println(err)
					http.Error(w, "データベースエラー", 500)
					return
				}
				context.Logs = append(context.Logs, lal)
			}
			rows2, err := db.Query("select count(*) from link_access_log")
			if err != nil {
				log.Println(err)
				http.Error(w, "データベースエラー", 500)
				return
			}
			defer rows2.Close()
			rows2.Next()
			err = rows2.Scan(&context.PageLength)
			if err != nil {
				log.Println(err)
				http.Error(w, "データベースエラー", 500)
				return
			}
			context.PageLength = context.PageLength / maxpageview
			if context.PageLength-context.PageLength/maxpageview*maxpageview > 0 {
				context.PageLength++
			}
		} else if r.URL.Path == "/import" {
			filename = "import"
		}
		if filename == "" {
			Page404(w)
			return
		}
		if filename != "index" {
			if !CheckLogin(r) {
				http.Redirect(w, r, "/", 303)
				return
			}
		}
		if err := template.Must(template.ParseFiles("template/"+filename+".html")).Execute(w, context); err != nil {
			log.Println(err)
			http.Error(w, "500", 500)
			return
		}
	} else {
		http.Error(w, "method not allowed", 405)
	}
}

func CheckLogin(r *http.Request) bool {
	cookie, err := r.Cookie("mmslogin")
	if err != nil {
		return false
	}
	db := database.Connect()
	defer db.Close()
	rows, err := db.Query("select * from admin_login where token = '" + database.Escape(cookie.Value) + "'")
	if err != nil {
		log.Println(err)
		return false
	}
	defer rows.Close()
	if !rows.Next() {
		return false
	}
	return true
}

func LogHandle(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed.", 405)
		return
	}

	//UA無しは通さない
	if r.UserAgent() == "" {
		http.Error(w, "Access Denied.", 403)
		return
	} else if strings.HasPrefix(r.UserAgent(), "curl/") {
		//curl禁止
		http.Error(w, "Access Denied.", 403)
		return
	} else if strings.HasPrefix(r.UserAgent(), "python-requests/") {
		//許さない
		http.Error(w, "Access Denied.", 403)
		return
	} else if strings.Index(r.UserAgent(), "AhrefsBot") > 0 {
		http.Error(w, "Access Denied.", 403)
	}

	p := r.URL.Path[len("/log/"):]
	if strings.Index(p, "/") <= 0 {
		http.Error(w, "", 404)
		return
	}
	email_id, err := strconv.Atoi(p[:strings.Index(p, "/")])
	if err != nil {
		http.Error(w, "email_id is not integer", 400)
		return
	}
	p = p[strings.Index(p, "/")+1:]
	if strings.Index(p, "/") <= 0 {
		http.Error(w, "", 404)
		return
	}
	customer_id, err := strconv.Atoi(p[:strings.Index(p, "/")])
	if err != nil {
		http.Error(w, "customer_id is not integer", 400)
		return
	}
	p = p[strings.Index(p, "/")+1:]
	link_number, err := strconv.Atoi(p)
	if err != nil {
		http.Error(w, "link_number is not integer", 400)
		return
	}
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	if xForwardedFor == "" {
		xForwardedFor = r.RemoteAddr
	}
	if xForwardedFor == "" {
		for k, v := range r.Header {
			if strings.ToLower(k) == "x-forwarded-for" {
				xForwardedFor += strings.Join(v, ",")
			}
		}
	}
	if strings.TrimSpace(r.FormValue("url")) == "" {
		http.Error(w, "redirect url is required", 400)
		return
	}
	db := database.Connect()
	defer db.Close()
	ins, err := db.Prepare("insert into link_access_log (email_id, link_number, customer_id, ua, ip) values (?, ?, ?, ?, ?)")
	if err != nil {
		log.Println(err)
		http.Redirect(w, r, r.FormValue("url"), 303)
		return
	}
	defer ins.Close()
	_, err = ins.Exec(email_id, link_number, customer_id, r.UserAgent(), xForwardedFor)
	if err != nil {
		log.Println(err)
		http.Redirect(w, r, r.FormValue("url"), 303)
		return
	}
	http.Redirect(w, r, r.FormValue("url"), 303)
}

func Page404(w http.ResponseWriter) {
	b, err := ioutil.ReadFile("template/404.html")
	if err != nil {
		log.Print(err)
		b = []byte("404 Page Not Found")
	}
	w.WriteHeader(404)
	fmt.Fprintf(w, string(b))
}

func GitHandle(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html; charset=utf-8")

	if r.Method == http.MethodGet {
		if err := template.Must(template.ParseFiles("template/git.html")).Execute(w, nil); err != nil {
			log.Println(err)
			http.Error(w, "500", 500)
			return
		}
	} else if r.Method == http.MethodPost {
		r.ParseMultipartForm(32 << 20)
		if r.FormValue("a") == "pull" {
			out, err := exec.Command("git", "pull").Output()
			if err != nil {
				log.Println(err)
				http.Error(w, err.Error(), 500)
				return
			}
			fmt.Fprintf(w, "<pre>"+string(out)+"</pre>")
		} else {
			http.Error(w, "?????", 400)
		}
	} else {
		http.Error(w, "method not allowed", 405)
	}
}

type Customer struct {
	Id        int             `json:"id"`
	Manager   string          `json:"manager"`
	Email     string          `json:"email"`
	Corp      string          `json:"corp"`
	Tel       string          `json:"tel"`
	Memo      string          `json:"memo"`
	CreatedAt string          `json:"created_at"`
	Groups    []CustomerGroup `json:"customer_group"`
}

type CustomerGroup struct {
	Id        int                   `json:"id"`
	GroupName string                `json:"group_name"`
	Members   []CustomerGroupMember `json:"members"`
}

type CustomerGroupMember struct {
	GroupId  int      `json:"group_id"`
	Customer Customer `json:"customer"`
}

type Email struct {
	Id          int           `json:"id"`
	GroupId     int           `json:"group_id"`
	GroupName   string        `json:"group_name"`
	Title       string        `json:"title"`
	Body        string        `json:"body"`
	SentAt      string        `json:"sent_at"`
	MemberCount int           `json:"member_count"`
	OpenCount   sql.NullInt64 `json:"open_count"`
	Members     string        `json:"members"`
}

type LinkAccessLog struct {
	EmailId    int    `json:"email_id"`
	LinkNumber int    `json:"link_number"`
	CustomerId int    `json:"customer_id"`
	UA         string `json:"ua"`
	IP         string `json:"ip"`
	AccessedAt string `json:"accessed_at"`
	EmailTitle string `json:"email_title"`
	Manager    string `json:"manager"`
}

func ApiHandle(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json; charset=utf-8")

	mode := ""
	if len(r.URL.Path) > len("/api/") {
		mode = r.URL.Path[len("/api/"):]
	}

	if r.Method == http.MethodGet {
		if mode == "customer" {
			db := database.Connect()
			defer db.Close()
			if r.FormValue("q") != "" {
				rows, err := db.Query("select id, manager, email from customer where manager like '%" + database.Escape(r.FormValue("q")) + "%' or email like '%" + database.Escape(r.FormValue("q")) + "%' order by id")
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "データベースエラー")
					return
				}
				defer rows.Close()
				ret := make([]Customer, 0)
				for rows.Next() {
					var c Customer
					err = rows.Scan(&c.Id, &c.Manager, &c.Email)
					if err != nil {
						log.Println(err)
						ApiResponse(w, false, "データベースエラー")
						return
					}
					ret = append(ret, c)
				}
				bytes, _ := json.Marshal(struct {
					Result   bool       `json:"result"`
					Customer []Customer `json:"customer"`
				}{
					Result:   true,
					Customer: ret,
				})
				fmt.Fprintln(w, string(bytes))
			} else {
				sort := "id"
				if r.FormValue("s") == "manager" {
					sort = "manager"
				} else if r.FormValue("s") == "corp" {
					sort = "corp"
				}
				page, err := strconv.Atoi(r.FormValue("p"))
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "ページ数が不正な値です")
					return
				}
				page--
				if page < 0 {
					page = 0
				}
				maxpageview := 30
				rows, err := db.Query("select id, manager, email, corp, tel, memo, created_at from customer order by " + sort + " limit " + strconv.Itoa(maxpageview) + " offset " + strconv.Itoa(page*maxpageview))
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "データベースのエラー")
					return
				}
				defer rows.Close()
				ret := make([]Customer, 0)
				for rows.Next() {
					var c Customer
					err = rows.Scan(&c.Id, &c.Manager, &c.Email, &c.Corp, &c.Tel, &c.Memo, &c.CreatedAt)
					if err != nil {
						log.Println(err)
						continue
					}
					ret = append(ret, c)
				}
				rows2, err := db.Query("select count(*) from customer")
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "データベースのエラー")
					return
				}
				defer rows2.Close()
				cnt := 0
				if rows2.Next() {
					rows2.Scan(&cnt)
				}
				pl := cnt / maxpageview
				if cnt-cnt/maxpageview*maxpageview > 0 {
					pl++
				}
				bytes, err := json.Marshal(struct {
					Result     bool       `json:"result"`
					Customer   []Customer `json:"customer"`
					PageLength int        `json:"page_length"`
				}{
					Result:     true,
					Customer:   ret,
					PageLength: pl,
				})
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, err.Error())
					return
				}
				fmt.Fprintln(w, string(bytes))
			}
			return
		} else if mode == "group" {
			db := database.Connect()
			defer db.Close()
			if r.FormValue("q") != "" {
				rows, err := db.Query("select id, gname from customer_group where gname like '%" + database.Escape(r.FormValue("q")) + "%' order by gname")
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "データベースエラー")
					return
				}
				defer rows.Close()
				ret := make([]CustomerGroup, 0)
				for rows.Next() {
					var cg CustomerGroup
					err = rows.Scan(&cg.Id, &cg.GroupName)
					if err != nil {
						log.Println(err)
						ApiResponse(w, false, "データベースエラー")
						return
					}
					ret = append(ret, cg)
				}
				bytes, _ := json.Marshal(struct {
					Result bool            `json:"result"`
					Groups []CustomerGroup `json:"group"`
				}{
					Result: true,
					Groups: ret,
				})
				fmt.Fprintln(w, string(bytes))
			} else {
				rows, err := db.Query("select group_id, customer_id, customer_group.id, gname, manager, email from customer_group_member left outer join customer on customer_id = customer.id left outer join customer_group on group_id = customer_group.id")
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "データベースエラー")
					return
				}
				defer rows.Close()
				groups := make([]CustomerGroup, 0)
				members := make([]CustomerGroupMember, 0)
				for rows.Next() {
					var m CustomerGroupMember
					var cg CustomerGroup
					err = rows.Scan(&m.GroupId, &m.Customer.Id, &cg.Id, &cg.GroupName, &m.Customer.Manager, &m.Customer.Email)
					if err != nil {
						log.Println(err)
						ApiResponse(w, false, "データベースエラー")
						return
					}
					exits := false
					for _, g := range groups {
						if g.Id == cg.Id {
							exits = true
							break
						}
					}
					if !exits {
						groups = append(groups, cg)
					}
					members = append(members, m)
				}
				for i := 0; i < len(groups); i++ {
					for _, mem := range members {
						if groups[i].Id == mem.GroupId {
							groups[i].Members = append(groups[i].Members, mem)
						}
					}
				}
				bytes, _ := json.Marshal(struct {
					Result bool            `json:"result"`
					Groups []CustomerGroup `json:"group"`
				}{
					Result: true,
					Groups: groups,
				})
				fmt.Fprintln(w, string(bytes))
			}
			return
		} else if mode == "member" {
			id, err := strconv.Atoi(r.FormValue("id"))
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "idが数値ではありません")
				return
			}
			db := database.Connect()
			defer db.Close()
			rows, err := db.Query("select manager, email from customer where id in (select customer_id from customer_group_member where group_id = " + strconv.Itoa(id) + ")")
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			defer rows.Close()
			ret := make([]Customer, 0)
			for rows.Next() {
				var c Customer
				err = rows.Scan(&c.Manager, &c.Email)
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "データベースエラー")
					return
				}
				ret = append(ret, c)
			}
			bytes, _ := json.Marshal(struct {
				Result    bool       `json:"result"`
				Customers []Customer `json:"customer"`
			}{
				Result:    true,
				Customers: ret,
			})
			fmt.Fprintln(w, string(bytes))
			return
		} else if mode == "mail" {
			if strings.TrimSpace(r.FormValue("id")) != "" {
				id, err := strconv.Atoi(r.FormValue("id"))
				if err != nil {
					ApiResponse(w, false, "idが数値ではありません。")
					return
				}
				db := database.Connect()
				defer db.Close()
				rows, err := db.Query(`select email.id, email.group_id, title, body, sent_at, gname, members, mem_cnt, opn_cnt
				from email
				left outer join customer_group on email.group_id = customer_group.id
				left outer join (
					select group_id, group_concat(manager separator ', ') as members
					from customer_group_member
					left outer join customer on customer_group_member.customer_id = customer.id
					group by group_id
				) as tbl on email.group_id = tbl.group_id
				left outer join (
					select group_id, count(*) as mem_cnt
					from customer_group_member
					group by group_id
				) as tbl2 on email.group_id = tbl2.group_id
				left outer join (
					select email_id, count(*) as opn_cnt
					from open_log
					group by email_id
				) as tbl3 on email.id = tbl3.email_id
				where email.id = ` + strconv.Itoa(id))
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "データベースエラー")
					return
				}
				defer rows.Close()
				var em Email
				if rows.Next() {
					err = rows.Scan(&em.Id, &em.GroupId, &em.Title, &em.Body, &em.SentAt, &em.GroupName, &em.Members, &em.MemberCount, &em.OpenCount)
					if err != nil {
						log.Println(err)
						ApiResponse(w, false, "データベースエラー")
						return
					}
				}
				bytes, _ := json.Marshal(struct {
					Result bool  `json:"result"`
					Email  Email `json:"email"`
				}{
					Result: true,
					Email:  em,
				})
				fmt.Fprintln(w, string(bytes))
				return
			} else {
				group, _ := strconv.Atoi(r.FormValue("group"))
				title := strings.TrimSpace(r.FormValue("title"))
				body := r.FormValue("body")
				db := database.Connect()
				defer db.Close()
				q := `select email.id, email.group_id, gname, title, body, sent_at, mem_cnt, opn_cnt
				from email
				left outer join customer_group on email.group_id = customer_group.id
				left outer join (
					select group_id, count(*) as mem_cnt from customer_group_member group by group_id
				) as tbl_mem on email.group_id = tbl_mem.group_id
				left outer join (
					select email_id, count(*) as opn_cnt from open_log group by email_id
				) as tbl_opn on email.id = tbl_opn.email_id`
				where := make([]string, 0)
				if group > 0 {
					where = append(where, "group_id = "+strconv.Itoa(group))
				}
				if title != "" {
					where = append(where, "title like '%"+database.Escape(title)+"%'")
				}
				if body != "" {
					where = append(where, "body like '%"+database.Escape(body)+"%'")
				}
				if len(where) > 0 {
					q += " where "
				}
				q += strings.Join(where, " and ")
				q += " order by sent_at desc"
				rows, err := db.Query(q)
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "データベースエラー")
					return
				}
				defer rows.Close()
				ret := make([]Email, 0)
				for rows.Next() {
					var e Email
					err = rows.Scan(&e.Id, &e.GroupId, &e.GroupName, &e.Title, &e.Body, &e.SentAt, &e.MemberCount, &e.OpenCount)
					if err != nil {
						log.Println(err)
						ApiResponse(w, false, "データベースエラー")
						return
					}
					ret = append(ret, e)
				}
				bytes, _ := json.Marshal(struct {
					Result bool    `json:"result"`
					Email  []Email `json:"email"`
				}{
					Result: true,
					Email:  ret,
				})
				fmt.Fprintln(w, string(bytes))
			}
			return
		}
		fmt.Fprintf(w, mode)
	} else if r.Method == http.MethodPost {
		r.ParseMultipartForm(32 << 20)
		if mode == "login" {
			if r.FormValue("pass") != os.Getenv("ADMIN_PASS") {
				ApiResponse(w, false, "パスワードが間違っています")
				return
			}
			tkn := createTokenRand(64)
			db := database.Connect()
			defer db.Close()
			q, err := db.Prepare("insert into admin_login (token) values (?)")
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "db error")
				return
			}
			defer q.Close()
			_, err = q.Exec(&tkn)
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "insert error")
				return
			}
			r, err := db.Query("delete from admin_login where `created_at` <= subtime(now(), '168:00:00')")
			if err != nil {
				log.Println(err)
			}
			r.Close()
			cookie := &http.Cookie{
				Name:     "mmslogin",
				Value:    tkn,
				Path:     "/",
				HttpOnly: true,
				MaxAge:   3600 * 24 * 7,
			}
			http.SetCookie(w, cookie)
			ApiResponse(w, true, "")
			return
		} else if mode == "customer" {
			manager := r.FormValue("manager") //required
			corp := r.FormValue("corp")
			email := r.FormValue("email") //required
			tel := r.FormValue("tel")
			memo := r.FormValue("memo")
			if strings.TrimSpace(manager) == "" || strings.TrimSpace(email) == "" {
				ApiResponse(w, false, "必須項目が入力されていません")
				return
			}
			if strings.Index(email, "@") <= 0 || strings.Index(email, ".") < 3 {
				ApiResponse(w, false, "メールアドレスの値が不正です")
				return
			}
			db := database.Connect()
			defer db.Close()
			ins, err := db.Prepare("insert into customer (manager, email, corp, tel, memo) values (?, ?, ?, ?, ?)")
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースのエラー")
				return
			}
			defer ins.Close()
			result, err := ins.Exec(&manager, &email, &corp, &tel, &memo)
			newid, err := result.LastInsertId()
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "ページを再読み込みしてください")
				return
			}
			bytes, err := json.Marshal(struct {
				Result   bool     `json:"result"`
				Customer Customer `json:"customer"`
			}{
				Result: true,
				Customer: Customer{
					Id:      int(newid),
					Manager: manager,
					Email:   email,
					Corp:    corp,
					Tel:     tel,
					Memo:    memo,
				},
			})
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, err.Error())
				return
			}
			fmt.Fprintln(w, string(bytes))
			return
		} else if mode == "group" {
			if strings.TrimSpace(r.FormValue("gname")) == "" {
				ApiResponse(w, false, "グループ名が入力されていません")
				return
			}
			members := make([]int, 0)
			for k, v := range r.MultipartForm.Value {
				if k == "member" {
					for _, m := range v {
						mi, err := strconv.Atoi(m)
						if err == nil {
							exist := false
							for _, a := range members {
								if a == mi {
									exist = true
									break
								}
							}
							if !exist {
								members = append(members, mi)
							}
						}
					}
				}
			}
			db := database.Connect()
			defer db.Close()
			ins1, err := db.Prepare("insert into customer_group (gname) values (?)")
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			defer ins1.Close()
			result, err := ins1.Exec(r.FormValue("gname"))
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			newid, err := result.LastInsertId()
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			for _, mi := range members {
				ins2, err := db.Prepare("insert into customer_group_member (group_id, customer_id) values (?, ?)")
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "メンバーの追加に失敗しました")
					return
				}
				ins2.Exec(newid, mi)
				ins2.Close()
			}
			ApiResponse(w, true, "")
			return
		} else if mode == "mail" {
			group, err := strconv.Atoi(r.FormValue("group"))
			if err != nil {
				ApiResponse(w, false, "不正な値が送られました。")
				return
			}
			title := r.FormValue("title")
			if strings.TrimSpace(title) == "" {
				ApiResponse(w, false, "タイトルが空です。")
				return
			}
			body := r.FormValue("body")
			if strings.TrimSpace(body) == "" {
				ApiResponse(w, false, "本文が空です。")
				return
			}
			db := database.Connect()
			defer db.Close()
			ins, err := db.Prepare("insert into email (group_id, title, body) values (?, ?, ?)")
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "送信履歴の追加に失敗しました。")
				return
			}
			defer ins.Close()
			result, err := ins.Exec(group, title, body)
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "送信履歴の追加に失敗しました。")
				return
			}
			newid, err := result.LastInsertId()
			rows, err := db.Query("select id, manager, email from customer where id in (select customer_id from customer_group_member where group_id = " + strconv.Itoa(group) + ")")
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			defer rows.Close()
			errorList := make([]string, 0)
			for rows.Next() {
				var c Customer
				err = rows.Scan(&c.Id, &c.Manager, &c.Email)
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "データベースエラー")
					return
				}
				src := r.Referer()
				body = LinkReplace(src[:strings.LastIndex(src, "/")], body, int(newid), c.Id)
				src = src[:strings.LastIndex(src, "/")] + "/img/" + strconv.Itoa(int(newid)) + "/" + strconv.Itoa(c.Id)
				body = `<div style="display: block; position: absolute; left: 0; top: 0px; z-index: -1;"><img src="` + src + `"></div>` + body
				err = SendMail(c.Manager, c.Email, title, body)
				if err != nil {
					log.Println(err)
					errorList = append(errorList, c.Email)
				}
			}
			if len(errorList) > 0 {
				ApiResponse(w, false, "以下のアドレスへの送信に失敗しました。\n"+strings.Join(errorList, "\n"))
				return
			}
			ApiResponse(w, true, "")
			return
		} else if mode == "import" {
			if r.FormValue("json") == "" {
				ApiResponse(w, false, "データが送信されていません")
				return
			}
			var list [][]string
			err := json.Unmarshal([]byte(r.FormValue("json")), &list)
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データの形式が間違っています")
				return
			}
			for _, col := range list {
				if len(col) != 5 {
					ApiResponse(w, false, "データの構成が間違っています")
					return
				}
			}
			db := database.Connect()
			defer db.Close()
			tx, err := db.Begin()
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			for _, col := range list {
				ins, err := tx.Prepare("insert into customer (manager, email, corp, tel, memo) values (?, ?, ?, ?, ?)")
				if err != nil {
					log.Println(err)
					tx.Rollback()
					ApiResponse(w, false, "データベースエラー")
					return
				}
				_, err = ins.Exec(col[0], col[1], col[2], col[3], col[4])
				if err != nil {
					log.Println(err)
					ins.Close()
					tx.Rollback()
					ApiResponse(w, false, "データベースエラー")
					return
				}
				ins.Close()
			}
			tx.Commit()
			ApiResponse(w, true, "")
			return
		}
		fmt.Fprintf(w, mode)
	} else if r.Method == http.MethodPut {
		if mode == "customer" {
			id := r.FormValue("id")           //required
			manager := r.FormValue("manager") //required
			corp := r.FormValue("corp")
			email := r.FormValue("email") //required
			tel := r.FormValue("tel")
			memo := r.FormValue("memo")
			if strings.TrimSpace(id) == "" || strings.TrimSpace(manager) == "" || strings.TrimSpace(email) == "" {
				ApiResponse(w, false, "必須項目が入力されていません")
				return
			}
			if strings.Index(email, "@") <= 0 || strings.Index(email, ".") < 3 {
				ApiResponse(w, false, "メールアドレスの値が不正です")
				return
			}
			db := database.Connect()
			defer db.Close()
			upd, err := db.Prepare("update customer set manager = ?, email = ?, corp = ?, tel = ?, memo = ? where id = ?")
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースのエラー")
				return
			}
			defer upd.Close()
			_, err = upd.Exec(&manager, &email, &corp, &tel, &memo, &id)
			ApiResponse(w, true, "")
			return
		} else if mode == "group" {
			id, err := strconv.Atoi(r.FormValue("id"))
			if err != nil {
				ApiResponse(w, false, "idが数値ではありません")
				return
			}
			if strings.TrimSpace(r.FormValue("gname")) == "" {
				ApiResponse(w, false, "グループ名が入力されていません")
				return
			}
			members := make([]int, 0)
			for k, v := range r.MultipartForm.Value {
				if k == "member" {
					for _, m := range v {
						mi, err := strconv.Atoi(m)
						if err == nil {
							exist := false
							for _, a := range members {
								if a == mi {
									exist = true
									break
								}
							}
							if !exist {
								members = append(members, mi)
							}
						}
					}
				}
			}
			db := database.Connect()
			defer db.Close()
			upd, err := db.Prepare("update customer_group set gname = ? where id = ?")
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			defer upd.Close()
			_, err = upd.Exec(r.FormValue("gname"), id)
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			del, err := db.Query("delete from customer_group_member where group_id = " + strconv.Itoa(id))
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "メンバーの変更に失敗しました。")
				return
			}
			del.Close()
			for _, mi := range members {
				ins2, err := db.Prepare("insert into customer_group_member (group_id, customer_id) values (?, ?)")
				if err != nil {
					log.Println(err)
					ApiResponse(w, false, "メンバーの変更に失敗しました")
					return
				}
				ins2.Exec(id, mi)
				ins2.Close()
			}
			ApiResponse(w, true, "")
			return
		}
		fmt.Fprintf(w, mode)
	} else if r.Method == http.MethodDelete {
		if strings.HasPrefix(mode, "customer/") {
			id, err := strconv.Atoi(mode[len("customer/"):])
			if err != nil {
				ApiResponse(w, false, "idが数値ではありません")
				return
			}
			db := database.Connect()
			defer db.Close()
			tx, err := db.Begin()
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			del, err := tx.Query("delete from customer where id = " + strconv.Itoa(id))
			if err != nil {
				log.Println(err)
				tx.Rollback()
				ApiResponse(w, false, "データベースエラー")
				return
			}
			del.Close()
			del, err = tx.Query("delete from customer_group_member where customer_id = " + strconv.Itoa(id))
			if err != nil {
				log.Println(err)
				tx.Rollback()
				ApiResponse(w, false, "データベースエラー")
				return
			}
			del.Close()
			err = tx.Commit()
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			ApiResponse(w, true, "")
			return
		} else if strings.HasPrefix(mode, "group/") {
			id, err := strconv.Atoi(mode[len("group/"):])
			if err != nil {
				ApiResponse(w, false, "idが数値ではありません")
				return
			}
			db := database.Connect()
			defer db.Close()
			tx, err := db.Begin()
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			del, err := tx.Query("delete from customer_group where id = " + strconv.Itoa(id))
			if err != nil {
				log.Println(err)
				tx.Rollback()
				ApiResponse(w, false, "データベースエラー")
				return
			}
			del.Close()
			del, err = tx.Query("delete from customer_group_member where group_id = " + strconv.Itoa(id))
			if err != nil {
				log.Println(err)
				tx.Rollback()
				ApiResponse(w, false, "データベースエラー")
				return
			}
			del.Close()
			err = tx.Commit()
			if err != nil {
				log.Println(err)
				ApiResponse(w, false, "データベースエラー")
				return
			}
			ApiResponse(w, true, "")
			return
		}
		fmt.Fprintf(w, mode)
	} else {
		http.Error(w, "Method not allowed.", 405)
	}
}

func ApiResponse(w http.ResponseWriter, result bool, msg string) {
	bytes, err := json.Marshal(struct {
		Result  bool   `json:"result"`
		Message string `json:"message"`
	}{
		Result:  result,
		Message: msg,
	})
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprintln(w, string(bytes))
}

func LinkReplace(host, body string, email_id, customer_id int) string {
	links := make([]string, 0)
	str := body
	for strings.Index(str, " href=\"") >= 0 {
		href := str[strings.Index(str, " href=\"")+len(" href=\""):]
		if strings.Index(href, "\"") >= 0 {
			href = href[:strings.Index(href, "\"")]
		}
		str = str[strings.Index(str, " href=\""+href)+len(" href=\""+href):]
		if strings.HasPrefix(href, "//") || strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
			links = append(links, href)
		}
	}
	for i := 0; i < len(links); i++ {
		newhref := host + "/log/" + strconv.Itoa(email_id) + "/" + strconv.Itoa(customer_id) + "/" + strconv.Itoa(i) + "?url="
		if strings.HasPrefix(links[i], "//") {
			newhref += "https:"
		}
		newhref += url.QueryEscape(links[i])
		body = strings.Replace(body, " href=\""+links[i], " href=\""+newhref, 1)
	}
	return body
}

func ImgHandle(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "image/png")

	p := r.URL.Path[len("/img/"):]
	if strings.Index(p, "/") > 0 && strings.Index(p, "/") == strings.LastIndex(p, "/") {
		email_id_str := p[:strings.Index(p, "/")]
		customer_id_str := p[strings.Index(p, "/")+1:]
		email_id, err := strconv.Atoi(email_id_str)
		if err != nil {
			http.Error(w, "email_id is not integer", 400)
			return
		}
		customer_id, err := strconv.Atoi(customer_id_str)
		if err != nil {
			http.Error(w, "customer_id is not integer", 400)
			return
		}
		xForwardedFor := r.Header.Get("X-Forwarded-For")
		if xForwardedFor == "" {
			xForwardedFor = r.RemoteAddr
		}
		if xForwardedFor == "" {
			for k, v := range r.Header {
				if strings.ToLower(k) == "x-forwarded-for" {
					xForwardedFor += strings.Join(v, ",")
				}
			}
		}
		db := database.Connect()
		defer db.Close()
		ins, err := db.Prepare("insert into open_log (email_id, customer_id, ua, ip) values (?, ?, ?, ?)")
		if err != nil {
			log.Println(err)
			http.Error(w, "データベースエラー", 500)
			return
		}
		defer ins.Close()
		_, err = ins.Exec(email_id, customer_id, r.UserAgent(), xForwardedFor)
		if err != nil {
			log.Println(err)
		}
		im, err := os.Open("./static/i.png")
		if err != nil {
			http.Error(w, "?", 500)
			return
		}
		defer im.Close()
		io.Copy(w, im)
		return
	}
	http.Error(w, "", 404)
}

func SendMail(manager, email, title, body string) error {
	auth := smtp.PlainAuth("", os.Getenv("MAIL_ADDRESS"), os.Getenv("MAIL_PASS"), os.Getenv("MAIL_SERVER"))
	msg := []byte("" +
		"From: " + os.Getenv("MAIL_SENDER") + "<" + os.Getenv("MAIL_ADDRESS") + ">\r\n" +
		"To: " + manager + "<" + email + ">\r\n" +
		encodeHeader("Subject", title) +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=\"utf-8\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		encodeBody(body) +
		"\r\n")

	err := smtp.SendMail(os.Getenv("MAIL_SERVER")+":"+os.Getenv("MAIL_PORT"), auth, os.Getenv("MAIL_ADDRESS"), []string{email}, msg)
	return err
}

func FaviconHandle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Add("Content-Type", "image/vnd.microsoft.icon")
		f, err := os.Open("static/favicon.ico")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer f.Close()
		io.Copy(w, f)
	} else {
		http.Error(w, "method not allowed", 405)
	}
}

func createTokenRand(chr int) string {
	ret := ""
	for i := 0; len(ret) < chr; i++ {
		rand.Seed(time.Now().UnixNano() + int64(i))
		chr := 48 + rand.Intn(75)
		if (chr >= 97 && chr <= 122) ||
			(chr >= 65 && chr <= 90) ||
			(chr >= 48 && chr <= 57) {
			ret += string(rune(chr))
		}
	}
	return ret
}

func encodeHeader(code string, subject string) string {
	// UTF8 文字列を指定文字数で分割する
	b := bytes.NewBuffer([]byte(""))
	strs := []string{}
	length := 13
	for k, c := range strings.Split(subject, "") {
		b.WriteString(c)
		if k%length == length-1 {
			strs = append(strs, b.String())
			b.Reset()
		}
	}
	if b.Len() > 0 {
		strs = append(strs, b.String())
	}
	// MIME エンコードする
	b2 := bytes.NewBuffer([]byte(""))
	b2.WriteString(code + ":")
	for _, line := range strs {
		b2.WriteString(" =?utf-8?B?")
		b2.WriteString(base64.StdEncoding.EncodeToString([]byte(line)))
		b2.WriteString("?=\r\n")
	}
	return b2.String()
}

// 本文を 76 バイト毎に CRLF を挿入して返す
func encodeBody(body string) string {
	b := bytes.NewBufferString(body)
	s := base64.StdEncoding.EncodeToString(b.Bytes())
	b2 := bytes.NewBuffer([]byte(""))
	for k, c := range strings.Split(s, "") {
		b2.WriteString(c)
		if k%76 == 75 {
			b2.WriteString("\r\n")
		}
	}
	return b2.String()
}
