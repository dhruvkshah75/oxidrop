use std::{env, fs};
use std::path::{Path, PathBuf};
use tracing::{info, warn, error};

pub fn init() -> Result<PathBuf, String>{
    // check if storage path exists or not
    // here the storage_path is either a pathBuf or a string i.e. error 
    let storage_path = match env::var("STORAGE_PATH") {
        Ok(path_str) => {
            // takes the path_str and tranforms it into a path
            let path = Path::new(&path_str);

            if !path.exists() {
                warn!("Storage Path does not exist. Creating {}", path_str);

                if let Err(e) = fs::create_dir(path) {
                    // return Error string when path couldnt be created 
                    let err_msg = format!("Failed to create storage directory: {}", e);
                    error!("{}", err_msg); // Use error! for failures
                    return Err(err_msg);
                }
            }
            else {
                info!("Storage path verified");
            }

            // success then return the pathBuf
            Ok(path.to_path_buf())
        }
        Err(e) => {
            let err_msg = format!("Could not resolve the Storage Path issue: {}", e);
            error!("{}", err_msg);
            Err(err_msg)
        }
    };

    storage_path
    
}

