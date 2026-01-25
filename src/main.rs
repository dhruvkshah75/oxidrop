mod storage;

use std::env;
use dotenvy::dotenv;

fn main() {
    let name = String::from("oxidrop");

    // loads the env var from the env file 
    // .expect(): if dotenv fails then it will crash and prompt this message 
    dotenv().expect("Critical Error: .env file not found!");

    match env::var("PORT") {
        Ok(port) => println!("{n} is running on port: {p}", n = name, p = port),
        Err(error) => println!("Couldn't load the port. Error: {e}",e = error),
    }

    // calling the storage init function
    storage::init(); 

    println!("Welcome to {}", name);

    let version = String::from("1.0.0");
    println!("{} version {v}", name, v = version);
}
