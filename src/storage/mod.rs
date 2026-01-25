use std::{env, fs};
use std::path::Path;


pub fn init() {
    // check if storage path exists or not
    match env::var("STORAGE_PATH") {
        Ok(path_str) => {
            // takes the path_str and tranforms it into a path
            let path = Path::new(&path_str);

            if !path.exists() {
                println!("Storage Path does not exist. Creating {}", path_str);

                if let Err(e) = fs::create_dir(path) {
                    // print an error as path could not be made
                    eprintln!("Failed to create storage directory: {}", e);
                }
            }
            else {
                println!("Storage path verified: {}", path_str);
            }

        }
        Err(e) => println!("Could not resolve the Storage Path issue: {}", e),
    }
}

