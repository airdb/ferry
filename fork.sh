#!/bin/sh

# Thank you for providing high-quality project code, phuslu.
# The replacement of relevant identifiers here is for the purpose of
# distinguishing and managing the project more easily.

os=`uname -s`

sed=sed
if [ `uname -s` == "Darwin" ]; then
	sed=gsed
fi

files=("liner.service.example" "liner.sh")
for file in "${files[@]}"; do
  if [ -e $file ]; then
    mv $file ${file/liner/ferry}
  fi
done

function replace() {
  for file in `grep --exclude-dir 'build' --exclude-dir '.git' --exclude "fork.sh" --exclude "*.mmdb" -rl "$1" .`; do
    $sed -i "s#$1#$2#g" $file
  done
}

replace 'C:/Users' '/home'
replace 'phus.lu' 'airdb.dev'
replace 'liner' 'ferry'
replace 'Liner' 'Ferry'
replace '/home/phuslu' '/home/airdb'
replace 'phuslu:' 'airdb:'
replace 'phuslu@' 'airdb@'
