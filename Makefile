buildon: main.go
	go build -o buildon main.go

run: buildon

clean:
	rm -f buildon

install: buildon
	mv buildon /usr/local/bin/buildon

.PHONY: clean run install

