use axum::{
    routing::get,
    routing::any, 
    Router, 
    extract::State
};
use std::fs;
use std::net::SocketAddr;
use crate::config::{Config};
use std::sync::Arc;

mod webdav;

// "State" refers to any data that needs to stay alive between different requests.

// the handler that lists all the files in the dir and needs the shared config
async fn list_files_handler(State(state): State<AppState>) -> String {
    let storage_path = &state.config.storage_path;

    match fs::read_dir(storage_path) {
        Ok(entries) => {
            let mut files = String::from("Oxidrop Storage contents: \n---\n");
            // this returns the files name 
            for entry in entries.flatten() {
                if let Ok(name) = entry.file_name().into_string() {
                    files.push_str(&format!("- {}\n", name));
                }
            }
            files
        },
        Err(_) => "Error: Could not access storage folder.".to_string(),
    }
}


// we can send a ping request to check if the server is live or not 
async fn ping_handler() -> &'static str {
    "pong\n" 
}

// Create a "Container" to hold both the Config and the WebDAV Engine
#[derive(Clone)]
struct AppState {
    config: Arc<Config>,
    dav_engine: dav_server::DavHandler,
}
// The Appstate which i defined is the shared memory of my server

// function that starts the axum server 
pub async fn start(config: Arc<Config>) {  // needs the config struct which is wrapped inside arc for details

    let dav_engine = webdav::create_dav_handler(Arc::clone(&config)); 

    // Create the combined state
    let state = AppState {
        config: Arc::clone(&config),
        dav_engine,
    };

    let app = Router::new()
                .route("/ping", get(ping_handler))
                // Register the route and inject the state
                .route("/files", get(list_files_handler))
                .route("/", any(webdav::dav_handler)) //root endpoint 
                // Fallback for the root of the dav drive
                .route("/dav", any(webdav::dav_handler))
                // Capture everything starting with /dav/ and send it to the handler
                .route("/dav/*path", any(webdav::dav_handler))
                // Catch-all for any other path (e.g., /folder/file.txt)
                .route("/*path", any(webdav::dav_handler))
                .with_state(state); // This is the "Bridge": The creation of the shared state
            
    // Instead of relying solely on get_addr(), force it to listen to your iPad's network
    let addr = SocketAddr::from(([0, 0, 0, 0], config.server_port)); 

    println!("Oxidrop Gateway is listening on http://{}", addr);

    // Create a tcp listener using tokio's async networking 
    let listner = tokio::net::TcpListener::bind(addr)
                .await
                .expect("Could not bind to port. Is it already in use?");

    // Start the axum server 
    // This process will run untill stopped 
    axum::serve(listner, app) 
                .await
                .expect("Server encountered a critical error");

}