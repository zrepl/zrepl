#!/bin/bash



while read line # we know this is not always correct...
do
    if [[ "$line" =~ nomap* ]]; then
        echo "NOMAP"
        continue
    fi

    echo "didmap/${line}"

    if [ "$1" == "stop" ]; then
        break
    fi

done
