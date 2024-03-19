module github.com/forensant/pakiki-core

go 1.16

require (
	github.com/StackExchange/wmi v0.0.0-20210224194228-fe8f1750fd46 // indirect
	github.com/alecthomas/chroma/v2 v2.13.0
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751
	github.com/andybalholm/brotli v1.0.6
	github.com/elazarl/goproxy v0.0.0-20211114080932-d06c3be7c11b // indirect
	github.com/forensant/goproxy v0.0.0-20230620193648-66cc989b2a48
	github.com/getsentry/sentry-go v0.23.0
	github.com/go-chi/chi v4.1.2+incompatible // indirect
	github.com/go-openapi/spec v0.20.7 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/google/uuid v1.3.0
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.5.0
	github.com/json-iterator/go v1.1.12
	github.com/kirsle/configdir v0.0.0-20170128060238-e45d2f54772f
	github.com/mattn/go-sqlite3 v1.14.6 // indirect
	github.com/pkg/errors v0.9.1
	github.com/projectdiscovery/gologger v1.1.4
	github.com/projectdiscovery/interactsh v0.0.7
	github.com/projectdiscovery/retryablehttp-go v1.0.2
	github.com/rs/xid v1.3.0
	github.com/sergi/go-diff v1.2.0
	github.com/shirou/gopsutil v3.21.2+incompatible
	github.com/swaggo/http-swagger v1.0.0
	github.com/swaggo/swag v1.8.5
	github.com/zalando/go-keyring v0.1.1
	gopkg.in/corvus-ch/zbase32.v1 v1.0.0
	gorm.io/driver/sqlite v1.1.4
	gorm.io/gorm v1.21.5
)

replace github.com/keybase/go-keychain => github.com/99designs/go-keychain v0.0.0-20191008050251-8e49817e8af4
