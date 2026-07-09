package db

import (
	"errors"

	"pvsk-pipeline/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func Open(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, errors.New("DATABASE_DSN is empty")
	}
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}

func Migrate(gdb *gorm.DB) error {
	if err := gdb.AutoMigrate(
		&models.Paper{},
		&models.PaperAsset{},
		&models.MaterialClass{},
		&models.PaperMaterialClass{},
		&models.LLMClassification{},
		&models.CrawlJob{},
		&models.CrawlLog{},
		&models.Material{},
		&models.Composition{},
		&models.Structure{},
		&models.Device{},
		&models.Measurement{},
	); err != nil {
		return err
	}
	return SeedMaterialClasses(gdb)
}

func SeedMaterialClasses(gdb *gorm.DB) error {
	for _, cls := range models.DefaultMaterialClasses {
		if err := gdb.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "code"}},
			DoUpdates: clause.AssignmentColumns([]string{"description", "updated_at"}),
		}).Create(&cls).Error; err != nil {
			return err
		}
	}
	return nil
}
