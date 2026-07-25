package svn

import (
	"context"
	"encoding/xml"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSVNCLI implements svnCLI for testing.
type mockSVNCLI struct {
	infoResult          *repoInfo
	infoErr             error
	listResult          []listEntry
	listErr             error
	listRecursiveResult []listEntry
	listRecursiveErr    error
	catResult           []byte
	catErr              error
	diffResult          []diffEntry
	diffErr             error

	// Tracking for assertions
	infoCalls []string
	listCalls []string
	catCalls  []struct {
		path string
		rev  int64
	}
	diffCalls []struct {
		path           string
		oldRev, newRev int64
	}
}

func (m *mockSVNCLI) Info(_ context.Context, _ string) (*repoInfo, error) {
	m.infoCalls = append(m.infoCalls, "info")
	if m.infoErr != nil {
		return nil, m.infoErr
	}
	if m.infoResult != nil {
		return m.infoResult, nil
	}
	return &repoInfo{Revision: 100, UUID: "test-uuid", Root: "svn://host/repo", URL: "svn://host/repo"}, nil
}

func (m *mockSVNCLI) List(_ context.Context, _, _ string) ([]listEntry, error) {
	m.listCalls = append(m.listCalls, "list")
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listResult, nil
}

func (m *mockSVNCLI) ListRecursive(_ context.Context, _, _ string) ([]listEntry, error) {
	if m.listRecursiveErr != nil {
		return nil, m.listRecursiveErr
	}
	return m.listRecursiveResult, nil
}

func (m *mockSVNCLI) Cat(_ context.Context, _, path string, rev int64) ([]byte, error) {
	m.catCalls = append(m.catCalls, struct {
		path string
		rev  int64
	}{path, rev})
	if m.catErr != nil {
		return nil, m.catErr
	}
	if m.catResult != nil {
		return m.catResult, nil
	}
	return []byte("# " + path), nil
}

func (m *mockSVNCLI) DiffSummarize(_ context.Context, _, path string, oldRev, newRev int64) ([]diffEntry, error) {
	m.diffCalls = append(m.diffCalls, struct {
		path           string
		oldRev, newRev int64
	}{path, oldRev, newRev})
	if m.diffErr != nil {
		return nil, m.diffErr
	}
	return m.diffResult, nil
}

// newTestConnector creates a Connector with a mock CLI.
func newTestConnector(mock *mockSVNCLI) *Connector {
	return &Connector{
		newCLIFunc: func(_ *Config) svnCLI { return mock },
	}
}

func testConfig() *types.DataSourceConfig {
	return &types.DataSourceConfig{
		Credentials: map[string]interface{}{
			"repo_url": "https://svn.example.com/repo",
			"username": "alice",
			"password": "secret",
		},
		ResourceIDs: []string{"/docs"},
	}
}

// --- XML parsing tests ---

func TestParseInfoXML(t *testing.T) {
	xmlStr := `<?xml version="1.0"?>
<info>
  <entry path="repo" revision="105">
    <url>https://svn.example.com/repo</url>
    <repository>
      <root>https://svn.example.com/repo</root>
      <uuid>abc-123-def</uuid>
    </repository>
    <commit revision="105">
      <author>alice</author>
      <date>2024-01-15T10:30:00.000000Z</date>
    </commit>
  </entry>
</info>`

	var info infoXML
	err := xml.Unmarshal([]byte(xmlStr), &info)
	require.NoError(t, err)

	require.Len(t, info.Entries, 1)
	e := info.Entries[0]
	assert.Equal(t, "105", e.Revision)
	assert.Equal(t, "abc-123-def", e.UUID)
	assert.Equal(t, "https://svn.example.com/repo", e.URL)
}

func TestParseListXML(t *testing.T) {
	xmlStr := `<?xml version="1.0"?>
<lists>
  <list path="svn://localhost/repo">
    <entry kind="dir">
      <name>docs</name>
      <commit revision="100">
        <author>alice</author>
        <date>2024-01-10T00:00:00.000000Z</date>
      </commit>
    </entry>
    <entry kind="file">
      <name>readme.md</name>
      <size>1024</size>
      <commit revision="99">
        <author>bob</author>
        <date>2024-01-09T00:00:00.000000Z</date>
      </commit>
    </entry>
  </list>
</lists>`

	var listing listXML
	err := xml.Unmarshal([]byte(xmlStr), &listing)
	require.NoError(t, err)

	require.Len(t, listing.Entries, 2)
	assert.Equal(t, "docs", listing.Entries[0].Name)
	assert.Equal(t, "dir", listing.Entries[0].Kind)
	assert.Equal(t, "readme.md", listing.Entries[1].Name)
	assert.Equal(t, "file", listing.Entries[1].Kind)
	assert.Equal(t, int64(1024), listing.Entries[1].Size)
}

func TestParseDiffSummarizeXML(t *testing.T) {
	xmlStr := `<?xml version="1.0"?>
<diff>
  <paths>
    <path kind="file" item="added" props="none">svn://localhost/repo/docs/new.md</path>
    <path kind="file" item="modified" props="none">svn://localhost/repo/docs/guide.md</path>
    <path kind="file" item="deleted" props="none">svn://localhost/repo/docs/old.md</path>
  </paths>
</diff>`

	var summary diffSummarizeXML
	err := xml.Unmarshal([]byte(xmlStr), &summary)
	require.NoError(t, err)

	require.Len(t, summary.Paths, 3)
	assert.Equal(t, "added", summary.Paths[0].Item)
	assert.Equal(t, "svn://localhost/repo/docs/new.md", summary.Paths[0].Path)
	assert.Equal(t, "modified", summary.Paths[1].Item)
	assert.Equal(t, "deleted", summary.Paths[2].Item)
}

// --- Connector method tests ---

func TestConnector_Type(t *testing.T) {
	c := NewConnector()
	assert.Equal(t, "svn", c.Type())
}

func TestConnector_ListResources_Root(t *testing.T) {
	mock := &mockSVNCLI{
		listResult: []listEntry{
			{Name: "docs", Kind: "dir"},
			{Name: "readme.md", Kind: "file", Size: 100},
		},
	}
	c := newTestConnector(mock)

	resources, err := c.ListResources(context.Background(), testConfig(), "")
	require.NoError(t, err)

	require.Len(t, resources, 2)
	assert.Equal(t, "/docs", resources[0].ExternalID)
	assert.Equal(t, "", resources[0].ParentID)
	assert.True(t, resources[0].HasChildren)
	assert.Equal(t, "/readme.md", resources[1].ExternalID)
	assert.Equal(t, "", resources[1].ParentID)
	assert.False(t, resources[1].HasChildren)
}

func TestConnector_ListResources_Subdirectory(t *testing.T) {
	mock := &mockSVNCLI{
		listResult: []listEntry{
			{Name: "architecture", Kind: "dir"},
		},
	}
	c := newTestConnector(mock)

	resources, err := c.ListResources(context.Background(), testConfig(), "/docs")
	require.NoError(t, err)

	require.Len(t, resources, 1)
	assert.Equal(t, "/docs/architecture", resources[0].ExternalID)
	assert.Equal(t, "/docs", resources[0].ParentID)
}

func TestConnector_FetchAll(t *testing.T) {
	mock := &mockSVNCLI{
		infoResult: &repoInfo{Revision: 100, UUID: "test-uuid"},
		listRecursiveResult: []listEntry{
			{Name: "readme.md", Kind: "file", Size: 50},
			{Name: "guide/intro.md", Kind: "file", Size: 200},
			{Name: "images/logo.png", Kind: "file", Size: 5000},
		},
	}
	c := newTestConnector(mock)

	config := testConfig()
	config.Settings = map[string]interface{}{
		"file_extensions": []interface{}{".md"},
	}

	items, err := c.FetchAll(context.Background(), config, []string{"/docs"})
	require.NoError(t, err)

	require.Len(t, items, 2) // .png filtered out
	assert.Equal(t, "/docs/readme.md", items[0].ExternalID)
	assert.Contains(t, items[0].Metadata, "channel")
	assert.Equal(t, types.ChannelSVN, items[0].Metadata["channel"])
	assert.Equal(t, "https://svn.example.com/repo/docs/readme.md", items[0].URL)
	// Folder hierarchy reconstructed from the file path under the synced node.
	assert.Equal(t, "docs", items[0].FolderPath)
	assert.Equal(t, "docs/guide", items[1].FolderPath)
}

func TestConnector_FetchAll_PartialError(t *testing.T) {
	mock := &mockSVNCLI{
		infoResult: &repoInfo{Revision: 100, UUID: "test-uuid"},
		listRecursiveResult: []listEntry{
			{Name: "good.md", Kind: "file", Size: 50},
			{Name: "bad.md", Kind: "file", Size: 50},
		},
		catErr: fmt.Errorf("svn cat failed"),
	}
	c := newTestConnector(mock)

	items, err := c.FetchAll(context.Background(), testConfig(), []string{"/docs"})

	// All files failed → error, not partial
	require.Error(t, err)
	assert.Empty(t, items)
}

func TestConnector_FetchAll_PartialSuccess(t *testing.T) {
	mock := &mockSVNCLI{
		infoResult: &repoInfo{Revision: 100, UUID: "test-uuid"},
		listRecursiveResult: []listEntry{
			{Name: "good.md", Kind: "file", Size: 50},
			{Name: "bad.md", Kind: "file", Size: 50},
			{Name: "alsogood.md", Kind: "file", Size: 50},
		},
	}
	// Make Cat fail only for "bad.md" by using a custom mock
	callCount := 0
	mock.catResult = nil
	c := &Connector{
		newCLIFunc: func(_ *Config) svnCLI {
			return &selectiveCatMock{
				mockSVNCLI: mock,
				failPredicate: func(path string) bool {
					callCount++
					return path == "/docs/bad.md"
				},
			}
		},
	}

	items, err := c.FetchAll(context.Background(), testConfig(), []string{"/docs"})

	require.Len(t, items, 2)
	var pfe *datasource.PartialFetchError
	require.ErrorAs(t, err, &pfe)
	assert.Len(t, pfe.Details, 1)
}

// selectiveCatMock wraps mockSVNCLI and fails Cat calls based on a predicate.
type selectiveCatMock struct {
	*mockSVNCLI
	failPredicate func(path string) bool
}

func TestConnector_FetchAll_OversizedSoftSkip(t *testing.T) {
	mock := &mockSVNCLI{
		infoResult: &repoInfo{Revision: 100, UUID: "test-uuid"},
		listRecursiveResult: []listEntry{
			{Name: "good.md", Kind: "file", Size: 50},
			// Size 0 = unknown from list; real size revealed only by bounded Cat
			{Name: "sneaky-big.md", Kind: "file", Size: 0},
		},
	}
	c := &Connector{
		newCLIFunc: func(_ *Config) svnCLI {
			return &errCatMock{
				mockSVNCLI: mock,
				errFor:     map[string]error{"/docs/sneaky-big.md": ErrFileTooLarge},
			}
		},
	}

	items, err := c.FetchAll(context.Background(), testConfig(), []string{"/docs"})

	// Oversized = soft skip: no error, other files returned
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "/docs/good.md", items[0].ExternalID)
}

// errCatMock returns per-path errors from Cat.
type errCatMock struct {
	*mockSVNCLI
	errFor map[string]error
}

func (s *errCatMock) Cat(ctx context.Context, repoURL, path string, rev int64) ([]byte, error) {
	if err, ok := s.errFor[path]; ok {
		return nil, err
	}
	return s.mockSVNCLI.Cat(ctx, repoURL, path, rev)
}

func (s *selectiveCatMock) Cat(ctx context.Context, repoURL, path string, rev int64) ([]byte, error) {
	if s.failPredicate(path) {
		return nil, fmt.Errorf("simulated failure for %s", path)
	}
	return s.mockSVNCLI.Cat(ctx, repoURL, path, rev)
}

func TestConnector_FetchIncremental_FirstSync(t *testing.T) {
	mock := &mockSVNCLI{
		infoResult: &repoInfo{Revision: 100, UUID: "test-uuid"},
		listRecursiveResult: []listEntry{
			{Name: "readme.md", Kind: "file", Size: 50},
		},
	}
	c := newTestConnector(mock)

	items, cursor, err := c.FetchIncremental(context.Background(), testConfig(), nil)
	require.NoError(t, err)

	assert.Len(t, items, 1)
	require.NotNil(t, cursor)
	assert.Equal(t, int64(100), cursor.ConnectorCursor["last_revision"])
}

func TestConnector_FetchIncremental_UUIDChangeFallback(t *testing.T) {
	mock := &mockSVNCLI{
		infoResult: &repoInfo{Revision: 200, UUID: "new-uuid"},
		listRecursiveResult: []listEntry{
			{Name: "readme.md", Kind: "file", Size: 50},
		},
	}
	c := newTestConnector(mock)

	cursor := &types.SyncCursor{
		ConnectorCursor: map[string]interface{}{
			"last_revision": float64(100),
			"repo_uuid":     "old-uuid",
		},
	}

	items, newCursor, err := c.FetchIncremental(context.Background(), testConfig(), cursor)
	require.NoError(t, err)

	assert.Len(t, items, 1)
	assert.Equal(t, "new-uuid", newCursor.ConnectorCursor["repo_uuid"])
}

func TestConnector_FetchIncremental_NoChanges(t *testing.T) {
	mock := &mockSVNCLI{
		infoResult: &repoInfo{Revision: 100, UUID: "test-uuid"},
	}
	c := newTestConnector(mock)

	cursor := &types.SyncCursor{
		ConnectorCursor: map[string]interface{}{
			"last_revision": float64(100),
			"repo_uuid":     "test-uuid",
		},
	}

	items, newCursor, err := c.FetchIncremental(context.Background(), testConfig(), cursor)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(100), newCursor.ConnectorCursor["last_revision"])
}

func TestConnector_FetchIncremental_WithDiffs(t *testing.T) {
	mock := &mockSVNCLI{
		infoResult: &repoInfo{Revision: 105, UUID: "test-uuid"},
		diffResult: []diffEntry{
			{Path: "docs/new.md", Type: "A"},
			{Path: "docs/changed.md", Type: "M"},
			{Path: "docs/gone.md", Type: "D"},
		},
	}
	c := newTestConnector(mock)

	cursor := &types.SyncCursor{
		ConnectorCursor: map[string]interface{}{
			"last_revision": float64(100),
			"repo_uuid":     "test-uuid",
		},
	}

	items, newCursor, err := c.FetchIncremental(context.Background(), testConfig(), cursor)
	require.NoError(t, err)
	require.NotNil(t, newCursor)

	// 2 content items (A, M) + 1 deleted (D)
	require.Len(t, items, 3)

	// Deleted item
	var deletedItems []types.FetchedItem
	for _, item := range items {
		if item.IsDeleted {
			deletedItems = append(deletedItems, item)
		}
	}
	require.Len(t, deletedItems, 1)
	assert.Equal(t, "/docs/gone.md", deletedItems[0].ExternalID)

	// Added/Modified items carry the reconstructed folder path.
	var contentItems []types.FetchedItem
	for _, item := range items {
		if !item.IsDeleted {
			contentItems = append(contentItems, item)
		}
	}
	require.Len(t, contentItems, 2)
	for _, ci := range contentItems {
		assert.Equal(t, "docs", ci.FolderPath)
	}

	assert.Equal(t, int64(105), newCursor.ConnectorCursor["last_revision"])
}

func TestNormalizeDiffPath(t *testing.T) {
	repoURL := "svn://host:3690/repo"
	repoRoot := "svn://host:3690/repo"

	tests := []struct {
		name     string
		diffPath string
		want     string
	}{
		{"full URL", "svn://host:3690/repo/docs/new.md", "/docs/new.md"},
		{"relative path", "docs/new.md", "/docs/new.md"},
		{"leading slash", "/docs/new.md", "/docs/new.md"},
		{"root only", "", "/"},
		{"whitespace", "  docs/new.md  ", "/docs/new.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDiffPath(tt.diffPath, repoRoot, repoURL)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildFilePath(t *testing.T) {
	assert.Equal(t, "/docs/readme.md", buildFilePath("/docs", "readme.md"))
	assert.Equal(t, "/docs/sub/intro.md", buildFilePath("/docs", "sub/intro.md"))
	assert.Equal(t, "/readme.md", buildFilePath("/", "readme.md"))
	assert.Equal(t, "/readme.md", buildFilePath("", "readme.md"))
}

func TestSVNFolderPath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{"root file with leading slash", "/readme.md", ""},
		{"root file without leading slash", "readme.md", ""},
		{"single directory", "/docs/readme.md", "docs"},
		{"nested directories", "/docs/api/readme.md", "docs/api"},
		{"trunk prefix preserved", "/trunk/docs/api/readme.md", "trunk/docs/api"},
		{"empty path", "", ""},
		{"just a slash", "/", ""},
		{"collapses double slashes", "/docs//api/readme.md", "docs/api"},
		{"resolves parent segments", "/docs/../api/readme.md", "api"},
		{"filename only no dir", "/.hidden", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, svnFolderPath(tt.filePath))
		})
	}
}

func TestBuildCursor(t *testing.T) {
	info := &repoInfo{Revision: 42, UUID: "abc"}
	cursor := buildCursor(info)

	assert.Equal(t, int64(42), cursor.ConnectorCursor["last_revision"])
	assert.Equal(t, "abc", cursor.ConnectorCursor["repo_uuid"])
	assert.False(t, cursor.LastSyncTime.IsZero())
}

func TestParseSVNDate(t *testing.T) {
	// SVN format with microseconds
	dt := parseSVNDate("2024-01-15T10:30:00.000000Z")
	assert.False(t, dt.IsZero())
	assert.Equal(t, 2024, dt.Year())

	// Standard RFC3339
	dt2 := parseSVNDate("2024-01-15T10:30:00Z")
	assert.False(t, dt2.IsZero())

	// Empty
	dt3 := parseSVNDate("")
	assert.True(t, dt3.IsZero())

	// Invalid
	dt4 := parseSVNDate("not-a-date")
	assert.True(t, dt4.IsZero())
}
