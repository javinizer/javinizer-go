module github.com/javinizer/javinizer-go/tests/fuseintegration

go 1.27

require (
	github.com/hanwen/go-fuse/v2 v2.12.0
	github.com/javinizer/javinizer-go v0.0.0
	github.com/spf13/afero v1.15.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/PuerkitoBio/goquery v1.8.1 // indirect
	github.com/andybalholm/cascadia v1.3.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gorm.io/gorm v1.25.5 // indirect
)

replace github.com/javinizer/javinizer-go => ../..
