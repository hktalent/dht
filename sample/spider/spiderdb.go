package spider

import (
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Product struct {
	gorm.Model
	Ip         string
	CreateTime time.Time
	LastTime   time.Time
	Count      uint
}

type SpiderDb struct {
	db *gorm.DB
}

// 初始化数据库连接
func (r SpiderDb) Init(szDbName string, tableMod Product) error {
	db, err := gorm.Open(sqlite.Open(szDbName), &gorm.Config{})
	if nil == err {
		r.db = db
		db.AutoMigrate(&tableMod{})
	}
	return err
}

func (r SpiderDb) Save(ip string) {
	db := r.db
	if err != nil {
		panic("failed to connect database")
	}

	// Create
	db.Create(&Product{Code: "D42", Price: 100})

	// Read
	var product Product
	// db.First(&product, 1)                 // find product with integer primary key
	db.First(&product, "ip = ?", ip) // find product with code D42

	// Update - update product's price to 200
	db.Model(&product).Update("Price", 200)
	// Update - update multiple fields
	db.Model(&product).Updates(Product{Price: 200, Code: "F42"}) // non-zero fields
	db.Model(&product).Updates(map[string]interface{}{"Price": 200, "Code": "F42"})

	// Delete - delete product
	db.Delete(&product, 1)
}
