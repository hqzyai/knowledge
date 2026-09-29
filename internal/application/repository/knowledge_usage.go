package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type KnowledgeUsageRepository struct{ db *gorm.DB }

func NewKnowledgeUsageRepository(db *gorm.DB) *KnowledgeUsageRepository {
	return &KnowledgeUsageRepository{db: db}
}

type recallEvent struct {
	ID         string
	EventID    string
	TenantID   uint64
	KBID       string
	Kind       string
	ResourceID string
	ActorID    string
	CreatedAt  time.Time
}

func (recallEvent) TableName() string { return "knowledge_recall_events" }

// API-key attribution uses the persisted key binding, never a caller-supplied
// external-user header or the tenant's synthetic fallback user.
func (r *KnowledgeUsageRepository) actor(ctx context.Context) (string, error) {
	if scope, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		var key struct{ Name string }
		if err := r.db.WithContext(ctx).Table("tenant_api_keys").Select("name").Where("id = ?", scope.KeyID).Take(&key).Error; err != nil {
			return "", err
		}
		if strings.HasPrefix(key.Name, types.ExternalUserAPIKeyNamePrefix) {
			return strings.TrimPrefix(key.Name, types.ExternalUserAPIKeyNamePrefix), nil
		}
		return fmt.Sprintf("api-key:%d", scope.KeyID), nil
	}
	if p, ok := types.PrincipalFromContext(ctx); ok {
		if p.Type == types.PrincipalWebUser {
			return p.ID, nil
		}
		return p.StorageID(), nil
	}
	return "unattributed", nil
}

// RecordRecall stores one library hit and one hit per distinct returned
// resource, atomically. Collection failure never breaks a successful search.
func (r *KnowledgeUsageRepository) RecordRecall(ctx context.Context, hits []types.RecallHit) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := r.record(ctx, hits); err != nil {
		logger.Errorf(ctx, "Knowledge recall collection failed: %v", err)
	}
}

func (r *KnowledgeUsageRepository) record(ctx context.Context, hits []types.RecallHit) error {
	actor, err := r.actor(ctx)
	if err != nil {
		return err
	}
	ids := map[string][]string{}
	for _, hit := range hits {
		if hit.Kind == "document" || hit.Kind == "wiki" {
			ids[hit.Kind] = append(ids[hit.Kind], hit.ResourceID)
		}
	}
	type resource struct {
		ID              string
		TenantID        uint64
		KnowledgeBaseID string
	}
	resources := map[string]resource{}
	for kind, list := range ids {
		table := "knowledges"
		if kind == "wiki" {
			table = "wiki_pages"
		}
		var rows []resource
		if err := r.db.WithContext(ctx).Table(table).Select("id, tenant_id, knowledge_base_id").Where("id IN ?", list).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			resources[kind+":"+row.ID] = row
		}
	}
	eventID, now := uuid.NewString(), time.Now().UTC()
	seen := map[string]bool{}
	rows := []recallEvent{}
	appendRow := func(resource resource, kind, id string) {
		key := resource.KnowledgeBaseID + ":" + kind + ":" + id
		if seen[key] {
			return
		}
		seen[key] = true
		rows = append(rows, recallEvent{ID: uuid.NewString(), EventID: eventID, TenantID: resource.TenantID, KBID: resource.KnowledgeBaseID, Kind: kind, ResourceID: id, ActorID: actor, CreatedAt: now})
	}
	for _, hit := range hits {
		resource, ok := resources[hit.Kind+":"+hit.ResourceID]
		if !ok || resource.KnowledgeBaseID == "" || hit.KnowledgeBaseID != "" && hit.KnowledgeBaseID != resource.KnowledgeBaseID {
			continue
		}
		appendRow(resource, "library", resource.KnowledgeBaseID)
		appendRow(resource, hit.Kind, resource.ID)
	}
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return tx.CreateInBatches(rows, 200).Error })
}

type KnowledgeUsageQuery struct {
	View         string
	KBID         string
	ResourceID   string
	ResourceType string
	Days         int
	Page         int
	PageSize     int
	Search       string
}
type KnowledgeUsageRow struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Recalls     int64   `json:"recalls"`
	Users       int64   `json:"users"`
	LastRecall  *string `json:"last_recall"`
	SourceCount int     `json:"source_count"`
	Deleted     bool    `json:"deleted"`
}
type KnowledgeUsageSummary struct {
	Libraries       int64 `json:"libraries"`
	Recalls         int64 `json:"recalls"`
	DocumentRecalls int64 `json:"document_recalls"`
	WikiRecalls     int64 `json:"wiki_recalls"`
	Users           int64 `json:"users"`
}
type KnowledgeUsageResult struct {
	Items               []KnowledgeUsageRow   `json:"items"`
	Total               int64                 `json:"total"`
	CollectionStartedAt time.Time             `json:"collection_started_at"`
	Summary             KnowledgeUsageSummary `json:"summary"`
	Library             *KnowledgeUsageRow    `json:"library,omitempty"`
	Resource            *KnowledgeUsageRow    `json:"resource,omitempty"`
}

func (r *KnowledgeUsageRepository) events(ctx context.Context, tenantID uint64, q KnowledgeUsageQuery) *gorm.DB {
	tx := r.db.WithContext(ctx).Table("knowledge_recall_events").Where("tenant_id = ?", tenantID)
	if q.Days > 0 {
		tx = tx.Where("created_at >= ?", time.Now().UTC().AddDate(0, 0, -q.Days))
	}
	if q.KBID != "" {
		tx = tx.Where("kb_id = ?", q.KBID)
	}
	if q.ResourceID != "" {
		tx = tx.Where("resource_id = ? AND kind = ?", q.ResourceID, q.ResourceType)
	}
	return tx
}

func (r *KnowledgeUsageRepository) Read(ctx context.Context, tenantID uint64, q KnowledgeUsageQuery) (*KnowledgeUsageResult, error) {
	result := &KnowledgeUsageResult{Items: []KnowledgeUsageRow{}}
	var collection struct{ StartedAt time.Time }
	if err := r.db.WithContext(ctx).Table("knowledge_recall_collection").Where("id = 1").Take(&collection).Error; err != nil {
		return nil, err
	}
	result.CollectionStartedAt = collection.StartedAt
	if q.KBID != "" {
		result.Library = &KnowledgeUsageRow{}
		if err := r.db.WithContext(ctx).Table("knowledge_bases").Select("id, name").Where("tenant_id = ? AND id = ?", tenantID, q.KBID).Take(result.Library).Error; err != nil {
			return nil, err
		}
	}
	var wiki usageWikiSources
	if q.ResourceID != "" {
		result.Resource = &KnowledgeUsageRow{}
		if q.ResourceType == "wiki" {
			if err := r.db.WithContext(ctx).Table("wiki_pages").Select("id, title, source_refs").Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, q.KBID, q.ResourceID).Take(&wiki).Error; err != nil {
				return nil, err
			}
			result.Resource.ID, result.Resource.Name = wiki.ID, wiki.Title
		} else if err := r.db.WithContext(ctx).Table("knowledges").Select("id, title AS name").Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, q.KBID, q.ResourceID).Take(result.Resource).Error; err != nil {
			return nil, err
		}
	}
	countExpr := "SUM(CASE WHEN kind = 'library' THEN 1 ELSE 0 END)"
	if q.ResourceID != "" {
		countExpr = "COUNT(*)"
	}
	if err := r.events(ctx, tenantID, q).Select("COUNT(DISTINCT kb_id) AS libraries, COALESCE(" + countExpr + ", 0) AS recalls, COALESCE(SUM(CASE WHEN kind = 'document' THEN 1 ELSE 0 END), 0) AS document_recalls, COALESCE(SUM(CASE WHEN kind = 'wiki' THEN 1 ELSE 0 END), 0) AS wiki_recalls, COUNT(DISTINCT actor_id) AS users").Scan(&result.Summary).Error; err != nil {
		return nil, err
	}
	if q.View == "sources" {
		return r.sources(ctx, tenantID, q, wiki, result)
	}

	var tx *gorm.DB
	if q.View == "users" {
		events := r.events(ctx, tenantID, q)
		if q.ResourceID == "" {
			events = events.Where("kind = ?", "library")
		}
		agg := events.Select("actor_id, COUNT(*) AS recalls, MAX(created_at) AS last_recall").Group("actor_id")
		tx = r.db.WithContext(ctx).Table("(?) AS a", agg).Joins("LEFT JOIN users AS u ON u.id = a.actor_id").Select("a.actor_id AS id, COALESCE(NULLIF(u.username, ''), a.actor_id) AS name, a.recalls, a.last_recall")
	} else {
		table, name, kind := "knowledge_bases", "name", "library"
		if q.View == "documents" {
			table, name, kind = "knowledges", "title", "document"
		}
		if q.View == "wikis" {
			table, name, kind = "wiki_pages", "title", "wiki"
		}
		agg := r.events(ctx, tenantID, q).Where("kind = ?", kind).Select("resource_id, COUNT(*) AS recalls, COUNT(DISTINCT actor_id) AS users, MAX(created_at) AS last_recall").Group("resource_id")
		tx = r.db.WithContext(ctx).Table(table+" AS r").Joins("LEFT JOIN (?) AS a ON a.resource_id = r.id", agg).Where("r.tenant_id = ? AND (r.deleted_at IS NULL OR a.recalls > 0)", tenantID)
		if kind != "library" {
			tx = tx.Where("r.knowledge_base_id = ?", q.KBID)
		}
		tx = tx.Select("r.id, r." + name + " AS name, COALESCE(a.recalls, 0) AS recalls, COALESCE(a.users, 0) AS users, a.last_recall, (r.deleted_at IS NOT NULL) AS deleted")
	}
	// Wrapping the projection keeps search aliases and pagination identical on PostgreSQL and SQLite.
	tx = r.db.WithContext(ctx).Table("(?) AS usage", tx)
	if q.Search != "" {
		tx = tx.Where("LOWER(name) LIKE ? OR LOWER(id) LIKE ?", "%"+strings.ToLower(q.Search)+"%", "%"+strings.ToLower(q.Search)+"%")
	}
	if err := tx.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := tx.Order("recalls DESC, name ASC, id ASC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Scan(&result.Items).Error; err != nil {
		return nil, err
	}
	// Aggregate timestamps lose SQLite's declared DATETIME affinity. Normalize
	// both database representations to the RFC3339 timestamps returned by PG.
	for i := range result.Items {
		if value := result.Items[i].LastRecall; value != nil {
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999"} {
				if parsed, err := time.Parse(layout, *value); err == nil {
					normalized := parsed.UTC().Format(time.RFC3339Nano)
					result.Items[i].LastRecall = &normalized
					break
				}
			}
		}
	}
	if q.View == "wikis" && len(result.Items) > 0 {
		ids := make([]string, 0, len(result.Items))
		for _, row := range result.Items {
			ids = append(ids, row.ID)
		}
		var pages []usageWikiSources
		if err := r.db.WithContext(ctx).Table("wiki_pages").Select("id, source_refs").Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ?", tenantID, q.KBID, ids).Find(&pages).Error; err != nil {
			return nil, err
		}
		counts := map[string]int{}
		for _, page := range pages {
			counts[page.ID] = len(page.SourceKnowledgeIDs())
		}
		for i := range result.Items {
			result.Items[i].SourceCount = counts[result.Items[i].ID]
		}
	}
	return result, nil
}

type usageWikiSources struct {
	ID         string
	Title      string
	SourceRefs string
}

func (w usageWikiSources) SourceKnowledgeIDs() []string {
	var refs []string
	if json.Unmarshal([]byte(w.SourceRefs), &refs) != nil {
		return nil
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, ref := range refs {
		if id := types.WikiSourceKnowledgeID(ref); id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids
}

func (r *KnowledgeUsageRepository) sources(ctx context.Context, tenantID uint64, q KnowledgeUsageQuery, wiki usageWikiSources, result *KnowledgeUsageResult) (*KnowledgeUsageResult, error) {
	ids := wiki.SourceKnowledgeIDs()
	if len(ids) == 0 {
		return result, nil
	}
	var docs []struct {
		ID        string
		Title     string
		DeletedAt gorm.DeletedAt
	}
	if err := r.db.WithContext(ctx).Table("knowledges").Select("id, title, deleted_at").Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ?", tenantID, q.KBID, ids).Scan(&docs).Error; err != nil {
		return nil, err
	}
	byID := map[string]KnowledgeUsageRow{}
	for _, doc := range docs {
		byID[doc.ID] = KnowledgeUsageRow{ID: doc.ID, Name: doc.Title, Deleted: doc.DeletedAt.Valid}
	}
	rows := []KnowledgeUsageRow{}
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
			row = KnowledgeUsageRow{ID: id, Name: id, Deleted: true}
		}
		if q.Search == "" || strings.Contains(strings.ToLower(row.Name+" "+row.ID), strings.ToLower(q.Search)) {
			rows = append(rows, row)
		}
	}
	result.Total = int64(len(rows))
	start := min((q.Page-1)*q.PageSize, len(rows))
	end := min(start+q.PageSize, len(rows))
	result.Items = rows[start:end]
	return result, nil
}
