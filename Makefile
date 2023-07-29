VOLUME_NAME = check-results

dockerBuild:
	docker build -t check-api .

createVolume:
	docker volume create $(VOLUME_NAME)

docker:
	docker run -v $(VOLUME_NAME):/app/results --name check-api-container check-api

extractFiles:
	docker cp check-api-container:/app/results /home/6jodeci/Documents/saina-test/results/DockerResults

.PHONY: dockerBuild createVolume docker extractFiles