package knowledge

import "github.com/shrewx/ginx"

// Router 注册本地知识管理用例。
var Router = ginx.NewRouter(ginx.Group("/knowledge"))

func init() {
	Router.Register(&PreviewChunking{})
	Router.Register(&GetLibraryEmbeddingProfile{})
	Router.Register(&UpdateLibraryEmbeddingProfile{})
	Router.Register(&TestLibraryEmbedding{})

	Router.Register(&ListLibraryActivity{})
	Router.Register(&GetLibraryExecutionDefaults{})
	Router.Register(&UpdateLibraryExecutionDefaults{})
	Router.Register(&GetLibraryDeletion{})
	Router.Register(&RecoverLibraryDeletion{})
	Router.Register(&DeleteLibrary{})
	Router.Register(&RetryJob{})
	Router.Register(&ResumeJob{})
	Router.Register(&CreatePage{})
	Router.Register(&CreateTheme{})
	Router.Register(&GetSource{})
	Router.Register(&GetRaw{})
	Router.Register(&WritePage{})
	Router.Register(&WriteTheme{})
	Router.Register(&Organize{})
	Router.Register(&ListJobs{})
	Router.Register(&GetJob{})
	Router.Register(&ListLibraries{})
	Router.Register(&GetLibrary{})
	Router.Register(&CreateLibrary{})
	Router.Register(&UpdateLibrary{})
	Router.Register(&ListSources{})
	Router.Register(&ListPages{})
	Router.Register(&ListPageDirectory{})
	Router.Register(&GetPageBySlug{})
	Router.Register(&GetWikiIndex{})
	Router.Register(&ListRevisions{})
	Router.Register(&GetPage{})
	Router.Register(&ListThemes{})
	Router.Register(&ImportSource{})
	Router.Register(&ArchiveSource{})
	Router.Register(&RestorePage{})
}
