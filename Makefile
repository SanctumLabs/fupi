ROOT_DIR:=$(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
include $(ROOT_DIR)/.make/common.mk

# database commands
include $(ROOT_DIR)/.make/database.mk

# utility commands
include $(ROOT_DIR)/.make/utils.mk

# docker commands
include $(ROOT_DIR)/.make/docker.mk

# dev commands
include $(ROOT_DIR)/.make/dev.mk

# test commands
include $(ROOT_DIR)/.make/test.mk

# CI commands
include $(ROOT_DIR)/.make/ci.mk

# load test commands
include $(ROOT_DIR)/.make/loadtest.mk
