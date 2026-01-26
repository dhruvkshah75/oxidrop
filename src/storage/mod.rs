use std::{env, fs};
use std::path::{Path, PathBuf};


pub fn init() -> Result<PathBuf, String>{
    // check if storage path exists or not
    // here the storage_path is either a pathBuf or a string i.e. error 
    let storage_path = match env::var("STORAGE_PATH") {
        Ok(path_str) => {
            // takes the path_str and tranforms it into a path
            let path = Path::new(&path_str);

            if !path.exists() {
                println!("Storage Path does not exist. Creating {}", path_str);

                if let Err(e) = fs::create_dir(path) {
                    // return Error string when path couldnt be created 
                    return Err(format!("Failed to create storage directory: {}", e));
                }
            }
            else {
                println!("Storage path verified");
            }

            // success then return the pathBuf
            Ok(path.to_path_buf())
        }
        Err(e) => Err(format!("Could not resolve the Storage Path issue: {}", e)),
    };

    storage_path
    
}

