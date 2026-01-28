use axum::{routing::get, Router};
use std::net::SocketAddr;
use crate::config::{Config};

// we can send a ping request to check if the server is live or not 
async fn ping_handler() -> &'static str {
    "pong\n" 
}

// function that starts the axum server 
pub async fn start(config: Config) {  // needs the config struct for details 

    let app = Router::new()
                .route("/ping", get(ping_handler));

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