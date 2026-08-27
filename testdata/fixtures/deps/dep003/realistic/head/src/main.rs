use serde::Serialize;
use smallcrate::compress;

fn main() {
    println!("{}", compress("hello"));
}
