$1 > max { max = $1; line = $0 }
END      { print max, line }
