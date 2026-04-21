## Docker Commands for Linux

To build and run the Docker container for the project, use the following commands:

```bash
# Build the Docker image
docker build -t sistema-distribuido .

# Run the Docker container
docker run -p 8080:8080 sistema-distribuido
```

Make sure you have Docker installed on your Linux system before running these commands.