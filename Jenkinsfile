@Library('jenkins.shared.library') _

pipeline {
  agent {
    label 'ubuntu_20_04_label'
  }
  tools {
    go "Go 1.17"
  }
  options {
    checkoutToSubdirectory('src/github.com/infobloxopen/db-controller')
  }
  environment {
    DIRECTORY = "src/github.com/infobloxopen/db-controller"
    GIT_VERSION = sh(script: "cd ${DIRECTORY} && git describe --always --long --tags",
                       returnStdout: true).trim()
    CHART_VERSION = "${env.GIT_VERSION}-j${env.BUILD_NUMBER}"
    TAG = "${env.GIT_VERSION}"
    GOPATH = "$WORKSPACE"
  }
  stages {
    stage("Setup") {
      steps {
        prepareBuild()
      }
    }
    stage("Run tests") {
      steps {
      withCredentials([string(credentialsId: 'GITHUB_TOKEN', variable: 'GitHub_PAT')]) {
          sh "echo machine github.com login $GitHub_PAT > ~/.netrc"
          sh "echo machine api.github.com login $GitHub_PAT >> ~/.netrc"
        }
		dir("$DIRECTORY") {
		  sh "sudo apt-get update"
		  sh "sudo apt-get -y install postgresql-client"
		  sh "echo 'db-controller-name' > .id"
		  sh "make test"
		  sh "sudo apt-get -y remove postgresql-client"
		}
      }
    }
    stage("Build db-controller image") {
      steps {
        dir("$DIRECTORY") {
          sh "make docker-build"
        }
      }
    }
    stage("Push db-controller image") {
      when {
        anyOf { buildingTag() }
      }
      steps {
        withDockerRegistry([credentialsId: "${env.JENKINS_DOCKER_CRED_ID}", url: ""]) {
          dir("$DIRECTORY") {
            sh "REGISTRY=infoblox make docker-push"
          }
        }
      }
    }
    stage('Push charts') {
      when {
        anyOf {
          buildingTag()
        }
      }
      steps {
        dir ("${WORKSPACE}/${DIRECTORY}") {
          withDockerRegistry([credentialsId: "dockerhub-bloxcicd", url: ""]) {
            withAWS(region:'us-east-1', credentials:'CICD_HELM') {
              sh "make push-chart"
              sh "make build-properties"
              sh "make push-chart-crd"
              sh "make build-properties-crd"
              archiveArtifacts artifacts: '*.tgz'
              archiveArtifacts artifacts: '*build.properties'
              sh "chmod a+xrw ${WORKSPACE}/${DIRECTORY}"
            }
          }
        }
      }
    }
  }
  post {
    success {
      dir("${WORKSPACE}/${DIRECTORY}") {
        // finalizeBuild is one of the Secure CICD helper methods
        finalizeBuild('', getFileList("*.properties"))
      }
    }
    cleanup {
      dir("$DIRECTORY") {
        sh "make clean || true"
      }
      cleanWs()
    }
  }
}
