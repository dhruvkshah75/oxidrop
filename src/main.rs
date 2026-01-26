use crate::config::Config;

mod config;
mod storage;


fn main() {

    let name = String::from("oxidrop");
    println!("Welcome to {}", name);

    let version = String::from("1.0.0");
    println!("{} version {v}", name, v = version);


    // calling the storage init function
    let config = Config::load();

    println!("Server starting on port: {}", config.server_port);
    println!("Storage initialized at: {:?}", config.storage_path);

}
