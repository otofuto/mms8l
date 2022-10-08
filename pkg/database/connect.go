package database

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

func Connect() *sql.DB {
	err := godotenv.Load()
	if err != nil {
		log.Println(err)
	}

	connectionstring := os.Getenv("DB_USER") + ":" + os.Getenv("DB_PASS") + "@" + os.Getenv("DB_HOST") + "/" + os.Getenv("DB_NAME") + ""
	db, err := sql.Open("mysql", connectionstring)
	if err != nil {
		fmt.Println(connectionstring)
		panic(err.Error())
	}
	//fmt.Println("connect db");

	return db
}

func registerTlsConfig(pemPath, tlsConfigKey string) (err error) {
	caCertPool := x509.NewCertPool()
	pem, err := ioutil.ReadFile(pemPath)
	if err != nil {
		return
	}

	if ok := caCertPool.AppendCertsFromPEM(pem); !ok {
		return errors.New("Failed to append PEM.")
	}
	mysql.RegisterTLSConfig(tlsConfigKey, &tls.Config{
		ClientCAs:          caCertPool,
		InsecureSkipVerify: true,
	})

	return
}

func Escape(str string) string {
	ret := strings.Replace(str, "\\", "\\\\", -1)
	ret = strings.Replace(ret, "\"", "\\\"", -1)
	ret = strings.Replace(ret, "'", "\\'", -1)
	ret = strings.Replace(ret, "\t", "\\t", -1)
	ret = strings.Replace(ret, "\r", "\\r", -1)
	ret = strings.Replace(ret, "\n", "\\n", -1)

	return ret
}

func Int64ToInt(i int64) int {
	if i < math.MinInt32 || i > math.MaxInt32 {
		return 0
	} else {
		return int(i)
	}
}
