use axum::{
    body::Body,
    extract::State,
    http::Request,
    response::IntoResponse,
};
use dav_server::{localfs::LocalFs, fakels::FakeLs, DavHandler};
use std::sync::Arc;
use crate::{config::Config, server::AppState};

// this handler creates a webdav engine pointing to laptop's storage 
// we dont use State(config) as this is not a handler it just creates a webdav server 
pub fn create_dav_handler(config: Arc<Config>) -> DavHandler {
    // setup a local filesystem backend 
    // parameters => path, public_read, public_write, public_delete
    let fs = LocalFs::new(&config.storage_path, true, true, true);

    // setup a fake lock system => required for ipadOS 
    let ls = FakeLs::new();

    DavHandler::builder()
        .filesystem(fs)
        .locksystem(ls)
        .build_handler()
}

pub async fn dav_handler(
    State(state): State<AppState>,    // share the dav handler engine from the shared space
    req: Request<Body>,               // // Capture the raw HTTP request from the iPad
) -> impl IntoResponse {
 
    // Let the engine generate the XML/File response
    state.dav_engine.handle(req).await 
} 
