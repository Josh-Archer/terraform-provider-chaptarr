package provider

import (
	"context"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

func bookReadOnlyDefinitions() []readOnlyDefinition {
	return []readOnlyDefinition{
		{name: "book_lookup", description: "Lookup book and edition candidates without adding, monitoring, searching for, or downloading media.", attributes: map[string]schema.Attribute{"term": schema.StringAttribute{Required: true}, "media_type": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("audiobook", "ebook")}}, "result_json": resultJSONAttribute()}, request: func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
			values := url.Values{"term": []string{stringInput(ctx, req, resp, "term", true)}}
			if value := stringInput(ctx, req, resp, "media_type", false); value != "" {
				values.Set("mediaType", value)
			}
			return queryPath("/api/v1/book/lookup", values)
		}, decode: jsonDecode},
		{name: "editions", description: "Read the edition catalog and narrator metadata for one local book without changing monitored-edition selection.", attributes: map[string]schema.Attribute{"book_id": schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "result_json": resultJSONAttribute()}, request: func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
			id, _ := intInput(ctx, req, resp, "book_id")
			return queryPath("/api/v1/edition", url.Values{"bookId": []string{strconv.FormatInt(id, 10)}})
		}, decode: jsonDecode},
		{name: "book_file", description: "Inspect bounded book-file metadata. This data source never edits tags, moves paths, or deletes media.", attributes: map[string]schema.Attribute{"book_file_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "author_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "book_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "media_type": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("audiobook", "ebook")}}, "unmapped": schema.BoolAttribute{Optional: true}, "result_json": resultJSONAttribute()}, request: bookFilesRequest, decode: jsonDecode},
		previewDefinition("rename_book_preview", "/api/v1/rename", "Preview proposed book-file renames without moving files."),
		previewDefinition("retag_book_preview", "/api/v1/retag", "Preview proposed book-file retags without writing tags."),
	}
}

func bookFilesRequest(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
	if id, ok := intInput(ctx, req, resp, "book_file_id"); ok {
		endpoint := "/api/v1/bookfile/" + strconv.FormatInt(id, 10)
		return endpoint, endpoint
	}
	values := url.Values{}
	for name, queryName := range map[string]string{"author_id": "authorId", "book_id": "bookId"} {
		if value, ok := intInput(ctx, req, resp, name); ok {
			values.Set(queryName, strconv.FormatInt(value, 10))
		}
	}
	if value := stringInput(ctx, req, resp, "media_type", false); value != "" {
		values.Set("mediaType", value)
	}
	if value, ok := boolInput(ctx, req, resp, "unmapped"); ok {
		values.Set("unmapped", strconv.FormatBool(value))
	}
	return queryPath("/api/v1/bookfile", values)
}

func previewDefinition(name, endpoint, description string) readOnlyDefinition {
	return readOnlyDefinition{name: name, description: description, attributes: map[string]schema.Attribute{"author_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "book_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "media_type": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("audiobook", "ebook")}}, "result_json": resultJSONAttribute()}, request: func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
		values := url.Values{}
		for attribute, queryName := range map[string]string{"author_id": "authorId", "book_id": "bookId"} {
			if value, ok := intInput(ctx, req, resp, attribute); ok {
				values.Set(queryName, strconv.FormatInt(value, 10))
			}
		}
		if value := stringInput(ctx, req, resp, "media_type", false); value != "" {
			values.Set("mediaType", value)
		}
		return queryPath(endpoint, values)
	}, decode: jsonDecode}
}
