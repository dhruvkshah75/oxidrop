use crate::config::Config;

mod config;
mod storage;
mod server;

#[tokio::main]
async fn main() {

    let name = String::from("oxidrop");
    println!("Welcome to {}", name);

    let version = String::from("1.0.0");
    println!("{} version {v}", name, v = version);


    // calling the storage init function
    let config = Config::load();

    let addr = config.get_addr();
    println!("Server listening on {}", addr); 
    println!("Storage initialized at: {:?}", config.storage_path);

    server::start(config).await;

}
