@Library('jenkins.shared.library') _

pipeline {
  agent {
    label 'ubuntu_20_04_label'
  }
  tools {
    go "Go 1.22.1"
  }
  stages {
    stage("Setup") {
      steps {
        prepareBuild()
        sh '''
        echo "Setting up the environment"
        echo "deb http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list
        wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo apt-key add -
        sudo apt-get update

        if ! which psql > /dev/null; then


          timeout 300 bash -c -- 'while sudo fuser /var/lib/dpkg/lock-frontend > /dev/null 2>&1
                            do
                              echo "Waiting to get lock /var/lib/dpkg/lock-frontend..."
                              sleep 5
                            done'
          sudo apt-get install -y postgresql-client-14
        fi

        '''
      }
    }
    stage("Run tests") {
      steps {
        withCredentials([string(credentialsId: 'GITHUB_TOKEN', variable: 'GitHub_PAT')]) {
            sh "echo machine github.com login $GitHub_PAT > ~/.netrc"
            sh "echo machine api.github.com login $GitHub_PAT >> ~/.netrc"
        }
        sh "echo 'db-controller-name' > .id"
        sh "psql --version"
        sh "make test"
      }
    }
    // stage("Build db-controller image") {
    //   steps {
    //     withCredentials([string(credentialsId: 'GITHUB_TOKEN', variable: 'GitHub_PAT')]) {
    //         sh "echo machine github.com login $GitHub_PAT > ~/.netrc"
    //         sh "echo machine api.github.com login $GitHub_PAT >> ~/.netrc"
    //     }
    //     dir("$DIRECTORY") {
    //       sh """
    //         echo cicd > .id
    //         make docker-build
    //         make docker-build-dbproxy
    //         make docker-build-dsnexec
    //       """
    //     }
    //   }
    // }
    // stage("Push db-controller image") {
    //   steps {
    //     withDockerRegistry([credentialsId: "${env.JENKINS_DOCKER_CRED_ID}", url: ""]) {
    //       dir("$DIRECTORY") {
    //         sh "REGISTRY=infoblox make docker-push"
    //         sh "REGISTRY=infoblox make docker-push-dbproxy"
    //         sh "REGISTRY=infoblox make docker-push-dsnexec"
    //       }
    //     }
    //   }
    // }
    stage('Push charts') {
      steps {
        withAWS(region:'us-east-1', credentials:'CICD_HELM') {
          sh """
            make package-charts
            make push-charts
            make build-properties
          """
          archiveArtifacts artifacts: '*.tgz'
          archiveArtifacts artifacts: '*build.properties'
        }
      }
    }
  }
  post {
    success {
      finalizeBuild('', getFileList("*.properties"))
    }
  }
}
