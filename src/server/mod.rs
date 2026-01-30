use axum::{
    routing::get, 
    Router, 
    extract::State
};
use std::fs;
use std::net::SocketAddr;
use crate::config::{Config};
use std::sync::Arc;

// "State" refers to any data that needs to stay alive between different requests.

// the handler that lists all the files in the dir and needs the shared config
async fn list_files_handler(State(config): State<Arc<Config>>) -> String {
    let storage_path = &config.storage_path;

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

// function that starts the axum server 
pub async fn start(config: Arc<Config>) {  // needs the config struct which is wrapped inside arc for details 

    let app = Router::new()
                .route("/ping", get(ping_handler))
                // Register the route and inject the state
                .route("/files", get(list_files_handler))
                .with_state(config.clone()); // This is the "Bridge": The creation of the shared state 

    let addr_str = config.get_addr();
    let addr: SocketAddr = addr_str
                .parse()
                .expect("Invalid HOST_ADDR or PORT. Please check your .env file");

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